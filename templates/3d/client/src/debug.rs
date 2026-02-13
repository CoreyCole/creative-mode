use bevy::ecs::query::QueryBuilder;
use bevy::ecs::world::FilteredEntityRef;
use bevy::prelude::*;
use bevy::reflect::serde::ReflectSerializer;
use bevy::reflect::{ReflectFromPtr, ReflectSerialize, TypeRegistration, TypeRegistry};
use core::ops::Deref;
use serde::Deserialize;
use serde_json::{json, Value};
use wasm_bindgen::prelude::*;

// --- Query Protocol ---

#[derive(Deserialize)]
#[serde(tag = "type")]
pub enum DebugQuery {
    #[serde(rename = "list")]
    List,
    #[serde(rename = "resource")]
    Resource { name: String },
    #[serde(rename = "query")]
    Query { components: Vec<String> },
}

// --- JS Bridge ---

/// Exclusive system — checks for a pending JS debug request each frame.
/// No-op when no request is pending (one JS global read).
pub fn process_debug_queries(world: &mut World) {
    let Some(window) = web_sys::window() else {
        return;
    };

    // Read request from window.__debugRequest
    let Ok(request_val) = js_sys::Reflect::get(&window, &JsValue::from_str("__debugRequest"))
    else {
        return;
    };

    if !request_val.is_string() {
        return;
    }
    let request_str = request_val.as_string().unwrap();

    // Clear request immediately
    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugRequest"),
        &JsValue::NULL,
    );

    // Parse and execute
    let result = match serde_json::from_str::<DebugQuery>(&request_str) {
        Ok(query) => execute_query(world, &query),
        Err(e) => json!({"error": format!("invalid query: {e}")}),
    };

    // Write response
    let _ = js_sys::Reflect::set(
        &window,
        &JsValue::from_str("__debugResponse"),
        &JsValue::from_str(&result.to_string()),
    );
}

// --- Query Engine ---

fn execute_query(world: &mut World, query: &DebugQuery) -> Value {
    let registry = world.resource::<AppTypeRegistry>().clone();
    let registry = registry.read();

    match query {
        DebugQuery::List => {
            // Only list types that have ReflectSerialize (filters out Bevy internals)
            let types: Vec<&str> = registry
                .iter()
                .filter(|reg| reg.data::<ReflectSerialize>().is_some())
                .map(|reg| reg.type_info().type_path_table().short_path())
                .collect();
            json!({ "types": types })
        }

        DebugQuery::Resource { name } => {
            let Some(registration) = find_type(&registry, name) else {
                return json!({"error": format!("type '{name}' not found")});
            };
            let type_id = registration.type_id();
            let Some(component_id) = world.components().get_resource_id(type_id) else {
                return json!({"error": format!("'{name}' is not a resource")});
            };
            let Some(ptr) = world.get_resource_by_id(component_id) else {
                return json!({"error": format!("resource '{name}' not present")});
            };
            let Some(reflect_from_ptr) = registration.data::<ReflectFromPtr>() else {
                return json!({"error": format!("'{name}' missing ReflectFromPtr")});
            };
            // SAFETY: ptr type matches — component_id derived from same type_id
            let reflected = unsafe { reflect_from_ptr.as_reflect(ptr) };
            match serialize_reflect(reflected, registration, &registry) {
                Ok(val) => json!({"name": name, "value": val}),
                Err(e) => json!({"error": format!("serialization failed: {e}")}),
            }
        }

        DebugQuery::Query { components } => {
            let mut resolved = Vec::new();
            for name in components {
                let Some(reg) = find_type(&registry, name) else {
                    return json!({"error": format!("type '{name}' not found")});
                };
                let Some(id) = world.components().get_id(reg.type_id()) else {
                    return json!({"error": format!("'{name}' is not a component")});
                };
                resolved.push((id, name.clone(), reg));
            }

            let mut builder = QueryBuilder::<FilteredEntityRef>::new(world);
            for (id, _, _) in &resolved {
                builder.ref_id(*id);
            }
            let mut query_state = builder.build();

            let mut entities = Vec::new();
            for entity_ref in query_state.iter(world) {
                let mut comps = serde_json::Map::new();
                for (id, name, reg) in &resolved {
                    if let Some(ptr) = entity_ref.get_by_id(*id) {
                        if let Some(rfp) = reg.data::<ReflectFromPtr>() {
                            // SAFETY: ptr type matches — component_id from same registration
                            let reflected = unsafe { rfp.as_reflect(ptr) };
                            if let Ok(val) = serialize_reflect(reflected, reg, &registry) {
                                comps.insert(name.clone(), val);
                            }
                        }
                    }
                }
                entities.push(json!({
                    "entity": entity_ref.id().index_u32(),
                    "components": comps,
                }));
            }

            json!({"entities": entities, "count": entities.len()})
        }
    }
}

/// Prefers native Serialize (clean output) via ReflectSerialize,
/// falls back to ReflectSerializer (type-tagged output).
fn serialize_reflect(
    reflected: &dyn Reflect,
    registration: &TypeRegistration,
    registry: &TypeRegistry,
) -> Result<Value, String> {
    if let Some(reflect_serialize) = registration.data::<ReflectSerialize>() {
        let serializable = reflect_serialize.get_serializable(reflected);
        return serde_json::to_value(serializable.deref()).map_err(|e| e.to_string());
    }
    let serializer = ReflectSerializer::new(reflected, registry);
    serde_json::to_value(&serializer).map_err(|e| e.to_string())
}

fn find_type<'a>(registry: &'a TypeRegistry, name: &str) -> Option<&'a TypeRegistration> {
    registry
        .iter()
        .find(|reg| reg.type_info().type_path_table().short_path() == name)
}

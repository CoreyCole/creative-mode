package dsutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SignalManager provides a structured way to manage Datastar signals
// with flat (non-namespaced) signal references ($property instead of $id.property).
type SignalManager struct {
	Signals     any
	DataSignals string // JSON string for data-signals attribute
}

// Signals creates a new SignalManager with flat signal references.
// The signalsStruct should be any struct with json tags for each property.
func Signals(signalsStruct any) *SignalManager {
	jsonBytes, err := json.Marshal(signalsStruct)
	if err != nil {
		jsonBytes = []byte("{}")
	}

	return &SignalManager{
		Signals:     signalsStruct,
		DataSignals: string(jsonBytes),
	}
}

// Signal returns a reference to a signal property: "$property".
func (sm *SignalManager) Signal(property string) string {
	return "$" + property
}

// Toggle returns a toggle expression for a boolean signal.
// Example: Toggle("open") returns "$open = !$open"
func (sm *SignalManager) Toggle(property string) string {
	ref := sm.Signal(property)
	return fmt.Sprintf("%s = !%s", ref, ref)
}

// Set returns a set expression for a signal property.
// Example: Set("value", "'hello'") returns "$value = 'hello'"
func (sm *SignalManager) Set(property, value string) string {
	return fmt.Sprintf("%s = %s", sm.Signal(property), value)
}

// SetString returns a set expression with proper quoting.
// Example: SetString("value", "hello") returns "$value = 'hello'"
func (sm *SignalManager) SetString(property, value string) string {
	return fmt.Sprintf("%s = '%s'", sm.Signal(property), value)
}

// Equals creates a comparison expression.
// Example: Equals("value", "option1") returns "$value === 'option1'"
func (sm *SignalManager) Equals(property, value string) string {
	return fmt.Sprintf("%s === '%s'", sm.Signal(property), value)
}

// NotEquals creates a not-equals comparison expression.
// Example: NotEquals("value", "option1") returns "$value !== 'option1'"
func (sm *SignalManager) NotEquals(property, value string) string {
	return fmt.Sprintf("%s !== '%s'", sm.Signal(property), value)
}

// Conditional returns a ternary expression.
// Example: Conditional("loading", "'Saving...'", "'Save'") returns "$loading ? 'Saving...' : 'Save'"
func (sm *SignalManager) Conditional(property, trueValue, falseValue string) string {
	return fmt.Sprintf("%s ? %s : %s", sm.Signal(property), trueValue, falseValue)
}

// ConditionalAction creates a safe conditional action using ternary operator.
// Example: ConditionalAction("evt.target === evt.currentTarget", "open", "false")
// Returns: "evt.target === evt.currentTarget ? ($open = false) : void 0"
func (sm *SignalManager) ConditionalAction(condition, property, value string) string {
	return fmt.Sprintf("%s ? (%s) : void 0", condition, sm.Set(property, value))
}

// DataClass creates a JSON object for data-class attributes from a map.
func (sm *SignalManager) DataClass(classConditions map[string]string) string {
	if len(classConditions) == 0 {
		return "{}"
	}

	var parts []string
	for className, condition := range classConditions {
		escapedClass := strings.ReplaceAll(className, "'", "\\'")
		parts = append(parts, fmt.Sprintf("'%s': %s", escapedClass, condition))
	}

	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

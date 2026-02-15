package utils

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SignalManager provides a structured way to manage Datastar signals
type SignalManager struct {
	ID          string `json:"id"`
	Signals     any    `json:"signals"`
	DataSignals string `json:"dataSignals"`
}

// Signals creates a new SignalManager instance with the given ID and signals struct
func Signals(id string, signalsStruct any) *SignalManager {
	sanitizedID := strings.ReplaceAll(id, "-", "_")
	nested := map[string]any{
		sanitizedID: signalsStruct,
	}
	jsonBytes, err := json.Marshal(nested)
	if err != nil {
		jsonBytes = []byte("{}")
	}
	return &SignalManager{
		ID:          sanitizedID,
		Signals:     signalsStruct,
		DataSignals: string(jsonBytes),
	}
}

// Signal returns a reference to a specific signal property
func (sm *SignalManager) Signal(property string) string {
	return fmt.Sprintf("$%s.%s", sm.ID, property)
}

// Toggle returns a toggle expression for a boolean signal property
func (sm *SignalManager) Toggle(property string) string {
	ref := sm.Signal(property)
	return fmt.Sprintf("%s = !%s", ref, ref)
}

// Set returns a set expression for a signal property
func (sm *SignalManager) Set(property, value string) string {
	return fmt.Sprintf("%s = %s", sm.Signal(property), value)
}

// SetString returns a set expression for a string signal property with proper quoting
func (sm *SignalManager) SetString(property, value string) string {
	return fmt.Sprintf("%s = '%s'", sm.Signal(property), value)
}

// Conditional returns a conditional expression for a signal property
func (sm *SignalManager) Conditional(property, trueValue, falseValue string) string {
	return fmt.Sprintf("%s ? %s : %s", sm.Signal(property), trueValue, falseValue)
}

// ConditionalAction creates a safe conditional action expression using ternary operator
func (sm *SignalManager) ConditionalAction(condition, property, value string) string {
	return fmt.Sprintf("%s ? (%s) : void 0", condition, sm.Set(property, value))
}

// ConditionalMultiAction creates a safe conditional expression with multiple actions
func (sm *SignalManager) ConditionalMultiAction(condition string, actions ...string) string {
	if len(actions) == 0 {
		return ""
	}
	actionsStr := ""
	for i, action := range actions {
		if i > 0 {
			actionsStr += ", "
		}
		actionsStr += action
	}
	return fmt.Sprintf("%s ? (%s) : void 0", condition, actionsStr)
}

// StateAction represents a state with a condition and associated actions
type StateAction struct {
	Condition string
	Actions   []string
}

// MultiStateConditional creates a chain of conditional expressions for handling multiple states
func (sm *SignalManager) MultiStateConditional(states []StateAction) string {
	if len(states) == 0 {
		return ""
	}
	result := ""
	for i, state := range states {
		if i > 0 {
			result += " : "
		}
		actionsStr := ""
		for j, action := range state.Actions {
			if j > 0 {
				actionsStr += ", "
			}
			actionsStr += action
		}
		isLastCondition := i == len(states)-1
		if isLastCondition && state.Condition == "true" {
			if len(state.Actions) == 1 {
				result += actionsStr
			} else {
				result += fmt.Sprintf("(%s)", actionsStr)
			}
		} else {
			if len(state.Actions) == 1 {
				result += fmt.Sprintf("%s ? %s", state.Condition, actionsStr)
			} else {
				result += fmt.Sprintf("%s ? (%s)", state.Condition, actionsStr)
			}
		}
	}
	return result
}

// DataClass creates a clean JSON object for data-class attributes
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

// Equals creates a comparison expression between a signal and a value
func (sm *SignalManager) Equals(property, value string) string {
	return fmt.Sprintf("%s === '%s'", sm.Signal(property), value)
}

// NotEquals creates a not-equals comparison expression
func (sm *SignalManager) NotEquals(property, value string) string {
	return fmt.Sprintf("%s !== '%s'", sm.Signal(property), value)
}

// TernaryClass creates a ternary expression for conditional CSS classes
func (sm *SignalManager) TernaryClass(property, trueClass, falseClass string) string {
	return fmt.Sprintf("%s ? '%s' : '%s'", sm.Signal(property), trueClass, falseClass)
}

// TernaryStyle creates a ternary expression for conditional inline styles
func (sm *SignalManager) TernaryStyle(property, trueStyle, falseStyle string) string {
	return fmt.Sprintf("%s ? '%s' : '%s'", sm.Signal(property), trueStyle, falseStyle)
}

package selectui

import "github.com/a-h/templ"

// SelectArgs configures the select component.
type SelectArgs struct {
	ID          string           // unique ID for signal namespacing
	Class       string           // additional CSS classes on the wrapper
	Placeholder string           // text shown when no value selected
	Options     []SelectOption   // available options
	OnChange    string           // Datastar expression fired after selection (e.g. "@post('/status/graph')")
	Attributes  templ.Attributes // passthrough HTML attributes
}

// SelectOption represents a single option in the select dropdown.
type SelectOption struct {
	Value string // signal value
	Label string // display text
}

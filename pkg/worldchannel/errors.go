package worldchannel

import "fmt"

// ErrMayorNameTaken is returned when a mayor name is already in use.
type ErrMayorNameTaken struct {
	Name                string
	ExistingChannelName string
}

func (e *ErrMayorNameTaken) Error() string {
	return fmt.Sprintf("mayor name %q is already taken (channel: %s)", e.Name, e.ExistingChannelName)
}

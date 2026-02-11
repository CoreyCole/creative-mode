package dsutil

import "fmt"

// GetSSENoCancel generates a @get() expression with requestCancellation disabled.
// This prevents Datastar from canceling the SSE connection on navigation.
func GetSSENoCancel(url string) string {
	return fmt.Sprintf("@get('%s',{requestCancellation: 'disabled'})", url)
}

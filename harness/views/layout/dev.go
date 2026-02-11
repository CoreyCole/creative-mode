package layout

import "os"

// isDevMode checks if the DEV_MODE environment variable is set.
func isDevMode() bool {
	return os.Getenv("DEV_MODE") == "true"
}

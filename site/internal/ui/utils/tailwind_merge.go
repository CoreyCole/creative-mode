package utils

import (
	twmerge "github.com/Oudwins/tailwind-merge-go"
)

// TwMerge combines Tailwind classes and resolves conflicts.
func TwMerge(classes ...string) string {
	return twmerge.Merge(classes...)
}

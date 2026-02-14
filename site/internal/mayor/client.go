package mayor

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// NewClient creates a new Anthropic client.
func NewClient(apiKey string) anthropic.Client {
	return anthropic.NewClient(option.WithAPIKey(apiKey))
}

// Model is the Claude model used for the mayor conversation.
var Model anthropic.Model = anthropic.ModelClaudeSonnet4_5_20250929

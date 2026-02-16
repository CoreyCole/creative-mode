package imagegen

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/genai"
)

// DefaultModel is the Gemini model used for image generation.
const DefaultModel = "gemini-2.5-flash-image"

// GeneratedImage holds the raw bytes and MIME type of a generated image.
type GeneratedImage struct {
	Data     []byte
	MIMEType string
}

// GenerateOptions configures image generation.
type GenerateOptions struct {
	AspectRatio  string // e.g. "16:9", "1:1"
	PromptSuffix string // appended to the prompt (e.g. chromakey instructions)
	Model        string // override default model
}

// Client wraps the Google Gen AI SDK for image generation.
type Client struct {
	client *genai.Client
	logger *slog.Logger
}

// NewClient creates a new Gemini image generation client.
// Returns nil, nil if apiKey is empty (feature disabled).
func NewClient(ctx context.Context, apiKey string, logger *slog.Logger) (*Client, error) {
	if apiKey == "" {
		return nil, nil //nolint:nilnil // nil client means feature disabled
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}

	return &Client{client: client, logger: logger}, nil
}

// Generate calls Gemini to generate an image from a text prompt.
// Returns the raw image bytes and detected MIME type.
func (c *Client) Generate(ctx context.Context, prompt string, opts GenerateOptions) (*GeneratedImage, error) {
	model := DefaultModel
	if opts.Model != "" {
		model = opts.Model
	}

	config := &genai.GenerateContentConfig{
		ResponseModalities: []string{"TEXT", "IMAGE"},
	}
	if opts.AspectRatio != "" {
		config.ImageConfig = &genai.ImageConfig{
			AspectRatio: opts.AspectRatio,
		}
	}

	fullPrompt := prompt
	if opts.PromptSuffix != "" {
		fullPrompt += opts.PromptSuffix
	}

	result, err := c.client.Models.GenerateContent(ctx, model, genai.Text(fullPrompt), config)
	if err != nil {
		return nil, fmt.Errorf("generate content: %w", err)
	}

	for _, candidate := range result.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part.InlineData == nil || part.InlineData.Data == nil {
				continue
			}
			return &GeneratedImage{
				Data:     part.InlineData.Data,
				MIMEType: DetectMIMEType(part.InlineData.Data),
			}, nil
		}
	}

	return nil, errors.New("no image in response")
}

// DetectMIMEType returns the MIME type based on magic bytes.
func DetectMIMEType(data []byte) string {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}

	if len(data) >= 4 && data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 &&
		data[3] == 0x46 {
		return "image/webp"
	}

	return "image/png"
}

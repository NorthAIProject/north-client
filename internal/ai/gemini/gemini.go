// Package gemini implements ai.Client on Google's Gemini models.
//
// Gemini is North's default because it ingests video, audio, and PDFs natively.
// The knowledge base and the form-analysis feature would otherwise need a
// separate transcription and OCR pipeline before the model saw anything.
package gemini

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// fileReadyTimeout bounds the wait for an uploaded file to finish processing.
// Gemini transcodes video before it can be referenced, and a long clip is not
// instant.
const fileReadyTimeout = 5 * time.Minute

// filePollInterval is how often upload processing is checked.
const filePollInterval = 2 * time.Second

type Client struct {
	client       *genai.Client
	defaultModel string
}

type Options struct {
	APIKey       string
	DefaultModel string
}

func New(ctx context.Context, opts Options) (*Client, error) {
	if opts.APIKey == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "gemini: API key is required")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  opts.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "gemini: create client")
	}

	model := opts.DefaultModel
	if model == "" {
		model = "gemini-2.5-pro"
	}

	return &Client{client: client, defaultModel: model}, nil
}

func (c *Client) Name() string { return "gemini" }

func (c *Client) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	resp, err := c.client.Models.GenerateContent(ctx, c.model(req), toContents(req.Messages), toConfig(req))
	if err != nil {
		return nil, apperr.Wrap(classify(err), "gemini: generate")
	}

	out := &ai.Response{Text: resp.Text()}
	if usage := resp.UsageMetadata; usage != nil {
		out.Usage = ai.Usage{
			InputTokens:  int(usage.PromptTokenCount),
			OutputTokens: int(usage.CandidatesTokenCount),
		}
	}
	if len(resp.Candidates) > 0 {
		out.FinishReason = string(resp.Candidates[0].FinishReason)
	}

	return out, nil
}

func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	stream := c.client.Models.GenerateContentStream(ctx, c.model(req), toContents(req.Messages), toConfig(req))

	out := make(chan ai.StreamChunk)

	go func() {
		defer close(out)

		// GenerateContentStream returns an iterator; the SDK stops it when the
		// yield function returns false, which is how a disconnected client
		// cancels an in-flight generation instead of paying for the rest of it.
		var usage *ai.Usage

		for resp, err := range stream {
			if err != nil {
				send(ctx, out, ai.StreamChunk{Err: apperr.Wrap(classify(err), "gemini: stream")})
				return
			}

			if meta := resp.UsageMetadata; meta != nil {
				usage = &ai.Usage{
					InputTokens:  int(meta.PromptTokenCount),
					OutputTokens: int(meta.CandidatesTokenCount),
				}
			}

			if text := resp.Text(); text != "" {
				if !send(ctx, out, ai.StreamChunk{Text: text}) {
					return
				}
			}
		}

		if usage != nil {
			send(ctx, out, ai.StreamChunk{Usage: usage})
		}
	}()

	return out, nil
}

func (c *Client) UploadFile(ctx context.Context, req ai.UploadRequest) (*ai.File, error) {
	file, err := c.client.Files.Upload(ctx, req.Reader, &genai.UploadFileConfig{
		MIMEType:    req.MIMEType,
		DisplayName: req.Name,
	})
	if err != nil {
		return nil, apperr.Wrap(classify(err), "gemini: upload %q", req.Name)
	}

	// Video is transcoded server-side. Referencing a file still in PROCESSING
	// fails the generation, so the upload is not done until Gemini says it is.
	ready, err := c.waitUntilActive(ctx, file)
	if err != nil {
		return nil, err
	}

	return &ai.File{
		URI:       ready.URI,
		MIMEType:  ready.MIMEType,
		ExpiresAt: ready.ExpirationTime,
	}, nil
}

func (c *Client) waitUntilActive(ctx context.Context, file *genai.File) (*genai.File, error) {
	deadline := time.Now().Add(fileReadyTimeout)

	for {
		switch file.State {
		case genai.FileStateActive:
			return file, nil
		case genai.FileStateFailed:
			reason := "unknown reason"
			if file.Error != nil && file.Error.Message != "" {
				reason = file.Error.Message
			}
			return nil, apperr.Wrap(apperr.ErrValidation, "gemini: processing failed: %s", reason)
		}

		if time.Now().After(deadline) {
			return nil, apperr.Wrap(apperr.ErrUnavailable, "gemini: file still processing after %s", fileReadyTimeout)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(filePollInterval):
		}

		refreshed, err := c.client.Files.Get(ctx, file.Name, nil)
		if err != nil {
			return nil, apperr.Wrap(classify(err), "gemini: check upload state")
		}
		file = refreshed
	}
}

func (c *Client) model(req ai.Request) string {
	if req.Model != "" {
		return req.Model
	}
	return c.defaultModel
}

func toContents(messages []ai.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(messages))

	for _, m := range messages {
		parts := make([]*genai.Part, 0, len(m.Parts))
		for _, p := range m.Parts {
			switch {
			case p.FileURI != "":
				parts = append(parts, &genai.Part{
					FileData: &genai.FileData{FileURI: p.FileURI, MIMEType: p.MIMEType},
				})
			case len(p.InlineData) > 0:
				parts = append(parts, &genai.Part{
					InlineData: &genai.Blob{Data: p.InlineData, MIMEType: p.MIMEType},
				})
			case p.Text != "":
				parts = append(parts, &genai.Part{Text: p.Text})
			}
		}
		if len(parts) == 0 {
			continue
		}
		contents = append(contents, &genai.Content{Role: string(m.Role), Parts: parts})
	}

	return contents
}

func toConfig(req ai.Request) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{}

	if req.System != "" {
		cfg.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: req.System}}}
	}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = int32(req.MaxTokens)
	}
	if req.ResponseSchema != nil {
		// Both fields are required: the schema alone does not switch the model
		// out of prose mode.
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseSchema = toSchema(req.ResponseSchema)
	}

	return cfg
}

func toSchema(s *ai.Schema) *genai.Schema {
	if s == nil {
		return nil
	}

	out := &genai.Schema{
		Type:             toSchemaType(s.Type),
		Description:      s.Description,
		Enum:             s.Enum,
		Required:         s.Required,
		PropertyOrdering: s.PropertyOrdering,
	}

	if s.Items != nil {
		out.Items = toSchema(s.Items)
	}
	if len(s.Properties) > 0 {
		out.Properties = make(map[string]*genai.Schema, len(s.Properties))
		for name, prop := range s.Properties {
			out.Properties[name] = toSchema(prop)
		}
	}

	return out
}

func toSchemaType(t ai.SchemaType) genai.Type {
	switch t {
	case ai.TypeObject:
		return genai.TypeObject
	case ai.TypeArray:
		return genai.TypeArray
	case ai.TypeInteger:
		return genai.TypeInteger
	case ai.TypeNumber:
		return genai.TypeNumber
	case ai.TypeBoolean:
		return genai.TypeBoolean
	default:
		return genai.TypeString
	}
}

// classify maps a provider error onto North's sentinels so callers can decide
// whether to retry without matching on message text.
func classify(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case containsAny(msg, "429", "RESOURCE_EXHAUSTED", "quota", "overloaded", "UNAVAILABLE", "503"):
		return fmt.Errorf("%w: %s", apperr.ErrUnavailable, msg)
	case containsAny(msg, "401", "403", "API key", "PERMISSION_DENIED"):
		return fmt.Errorf("%w: %s", apperr.ErrForbidden, msg)
	default:
		return err
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func send(ctx context.Context, out chan<- ai.StreamChunk, chunk ai.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

var _ ai.Client = (*Client)(nil)

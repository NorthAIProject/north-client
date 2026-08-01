// Package fake is an AI client that answers from a script.
//
// It exists so that every service above internal/ai can be tested offline,
// deterministically, and for free. A test that needs the coach to return a
// malformed workout plan can simply say so, which is not something a real
// provider will reliably do on request.
package fake

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// Client is a scripted ai.Client.
//
// The zero value is usable and echoes a fixed reply. Set Responses to script a
// sequence, or Handler for full control.
type Client struct {
	// Responses are returned in order, one per call. When they run out the
	// last one repeats, so a test that makes an unexpected extra call gets a
	// sensible answer instead of a panic.
	Responses []Response

	// Handler, when set, overrides Responses entirely.
	Handler func(ctx context.Context, req ai.Request) (Response, error)

	// ChunkDelay is slept between streamed chunks. Zero in almost every test;
	// useful for exercising a client that disconnects mid-stream.
	ChunkDelay time.Duration

	// UploadError, when set, is returned by UploadFile.
	UploadError error

	mu       sync.Mutex
	calls    []ai.Request
	uploads  []ai.UploadRequest
	nextResp int
}

// Response is one scripted reply.
type Response struct {
	// Text is the reply. For a streamed call it is split into chunks.
	Text string

	// Chunks, when set, overrides the automatic splitting of Text, so a test
	// can control exactly how a reply arrives.
	Chunks []string

	// Err is returned instead of a reply.
	Err error

	Usage ai.Usage
}

func New(responses ...Response) *Client {
	return &Client{Responses: responses}
}

// Text is shorthand for a client that always answers with one string.
func Text(reply string) *Client {
	return &Client{Responses: []Response{{Text: reply}}}
}

func (c *Client) Name() string { return "fake" }

func (c *Client) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	resp, err := c.next(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Err != nil {
		return nil, resp.Err
	}
	return &ai.Response{Text: resp.Text, Usage: resp.Usage, FinishReason: "stop"}, nil
}

func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	resp, err := c.next(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan ai.StreamChunk)

	go func() {
		defer close(out)

		if resp.Err != nil {
			send(ctx, out, ai.StreamChunk{Err: resp.Err})
			return
		}

		for _, chunk := range resp.chunks() {
			if c.ChunkDelay > 0 {
				select {
				case <-ctx.Done():
					send(ctx, out, ai.StreamChunk{Err: ctx.Err()})
					return
				case <-time.After(c.ChunkDelay):
				}
			}
			if !send(ctx, out, ai.StreamChunk{Text: chunk}) {
				return
			}
		}

		usage := resp.Usage
		send(ctx, out, ai.StreamChunk{Usage: &usage})
	}()

	return out, nil
}

func (c *Client) UploadFile(ctx context.Context, req ai.UploadRequest) (*ai.File, error) {
	// Drain the reader so a test that passes a real file sees it consumed, and
	// so the byte count is available for assertions.
	n, _ := io.Copy(io.Discard, req.Reader)

	c.mu.Lock()
	c.uploads = append(c.uploads, req)
	c.mu.Unlock()

	if c.UploadError != nil {
		return nil, c.UploadError
	}

	return &ai.File{
		URI:       fmt.Sprintf("fake://uploads/%s?bytes=%d", req.Name, n),
		MIMEType:  req.MIMEType,
		ExpiresAt: time.Now().Add(48 * time.Hour),
	}, nil
}

// Calls returns every request the client has received, so a test can assert on
// what the prompt builder actually produced.
func (c *Client) Calls() []ai.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ai.Request(nil), c.calls...)
}

// LastCall returns the most recent request, or the zero value if there was none.
func (c *Client) LastCall() ai.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return ai.Request{}
	}
	return c.calls[len(c.calls)-1]
}

// Uploads returns every upload the client has received.
func (c *Client) Uploads() []ai.UploadRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ai.UploadRequest(nil), c.uploads...)
}

func (c *Client) next(ctx context.Context, req ai.Request) (Response, error) {
	c.mu.Lock()
	c.calls = append(c.calls, req)
	c.mu.Unlock()

	if c.Handler != nil {
		return c.Handler(ctx, req)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.Responses) == 0 {
		return Response{Text: "This is a fake coach reply."}, nil
	}

	i := c.nextResp
	if i >= len(c.Responses) {
		i = len(c.Responses) - 1 // repeat the last scripted answer
	} else {
		c.nextResp++
	}
	return c.Responses[i], nil
}

// chunks splits a reply for streaming. Word-by-word rather than one blob,
// because a test asserting "the UI received more than one chunk" is asserting
// something real about streaming.
func (r Response) chunks() []string {
	if len(r.Chunks) > 0 {
		return r.Chunks
	}
	if r.Text == "" {
		return nil
	}

	words := strings.SplitAfter(r.Text, " ")
	out := make([]string, 0, len(words))
	for _, w := range words {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// send delivers a chunk unless the caller has gone away, and reports whether
// the stream should continue.
func send(ctx context.Context, out chan<- ai.StreamChunk, chunk ai.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// compile-time check that the fake satisfies the interface it stands in for.
var _ ai.Client = (*Client)(nil)

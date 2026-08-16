package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/NorthAIProject/north-client/internal/messaging"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// apiBase is Telegram's Bot API root. A field on Client rather than a constant
// so a test can point it at a local server.
const apiBase = "https://api.telegram.org"

// maxMessageRunes is Telegram's limit on a single message.
//
// A coached reply can exceed it, and the API rejects the whole message rather
// than truncating, so North splits instead — a person reading half an answer
// is better served than one reading none of it.
const maxMessageRunes = 4096

// requestTimeout bounds one Bot API call. Generous for a phone network,
// bounded so a hung send does not hold a goroutine open forever.
const requestTimeout = 30 * time.Second

// Client talks to the Telegram Bot API. It implements messaging.Transport.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
	logger  *slog.Logger
}

func NewClient(token string) *Client {
	return &Client{
		token:   token,
		baseURL: apiBase,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) Platform() string { return messaging.PlatformTelegram }

// Send delivers a reply, splitting it if Telegram will not take it whole.
//
// Buttons ride on the last part only: an inline keyboard attached to the first
// chunk of a long answer would ask the person to decide before they have
// finished reading it.
func (c *Client) Send(ctx context.Context, externalID string, msg messaging.OutboundMessage) error {
	if msg.Silent {
		return nil
	}

	chat, err := chatIDFromString(externalID)
	if err != nil {
		return err
	}

	// Split on the raw text, then format each piece: the limit Telegram
	// enforces is on what a person reads, and tags do not count towards it.
	parts := splitMessage(msg.Text, maxMessageRunes)
	for i, part := range parts {
		body := map[string]any{
			"chat_id":    chat,
			"text":       markdownToHTML(part),
			"parse_mode": "HTML",
		}
		if last := i == len(parts)-1; last && len(msg.Options) > 0 {
			body["reply_markup"] = inlineKeyboard(msg.Options)
		}

		err := c.call(ctx, "sendMessage", body, nil)
		if err == nil {
			continue
		}

		// Telegram rejects the whole message when it cannot parse the markup,
		// so a single stray marker would otherwise cost the person their entire
		// answer. Slightly plain prose is a much smaller loss, and this is the
		// part of the formatting that actually matters.
		c.log().Warn("telegram refused formatted text; resending it plain", "error", err)

		delete(body, "parse_mode")
		body["text"] = stripMarkdown(part)
		if err := c.call(ctx, "sendMessage", body, nil); err != nil {
			return err
		}
	}
	return nil
}

// log is the client's logger, defaulted rather than required: a Client is
// constructed from a token alone in several places and none of them should have
// to think about logging to send a message.
func (c *Client) log() *slog.Logger {
	if c.logger != nil {
		return c.logger
	}
	return slog.Default()
}

// Typing shows the "typing…" indicator. Best effort: a failure here is not
// worth failing a reply over, so the error is returned and callers log it.
//
// Telegram clears the indicator after about five seconds, and a coached reply
// takes longer, so this is a signal that the message landed rather than an
// accurate progress bar.
func (c *Client) Typing(ctx context.Context, externalID string) error {
	chat, err := chatIDFromString(externalID)
	if err != nil {
		return err
	}
	return c.call(ctx, "sendChatAction", map[string]any{
		"chat_id": chat,
		"action":  "typing",
	}, nil)
}

// RegisterCommands publishes the command menu Telegram clients offer.
//
// Cosmetic, and best effort: the commands work whether or not this succeeds,
// because they are matched from the message text. What it buys is discovery —
// a person who does not know /unlink exists will never type it.
//
// The list comes from messaging rather than being spelled out here, so the menu
// cannot drift from what is actually handled.
func (c *Client) RegisterCommands(ctx context.Context) error {
	commands := messaging.Commands()

	entries := make([]map[string]string, 0, len(commands))
	for _, cmd := range commands {
		entries = append(entries, map[string]string{
			"command":     cmd.Name,
			"description": cmd.Description,
		})
	}
	return c.call(ctx, "setMyCommands", map[string]any{"commands": entries}, nil)
}

// Leave removes the bot from a chat.
//
// Used for groups and channels, which North refuses: a group has one chat id
// shared by everybody in it, so staying would leave an id available to be
// linked to somebody's account.
func (c *Client) Leave(ctx context.Context, externalID string) error {
	chat, err := chatIDFromString(externalID)
	if err != nil {
		return err
	}
	return c.call(ctx, "leaveChat", map[string]any{"chat_id": chat}, nil)
}

// AnswerCallback dismisses the loading spinner on a tapped button.
//
// Telegram spins that button until this is called, so skipping it leaves the
// person looking at a control that appears stuck even though the work started.
func (c *Client) AnswerCallback(ctx context.Context, callbackID string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": callbackID,
	}, nil)
}

// getUpdates long-polls for new updates. Used only in polling mode.
func (c *Client) getUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]update, error) {
	var result []update
	body := map[string]any{
		"timeout":         timeoutSeconds,
		"allowed_updates": []string{"message", "callback_query"},
	}
	if offset > 0 {
		body["offset"] = offset
	}
	if err := c.call(ctx, "getUpdates", body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// call posts one Bot API method.
//
// The token is in the URL path, which is how the Bot API authenticates. That
// is exactly why no error here ever includes the URL: an error string is the
// one place a credential reliably ends up in a log.
func (c *Client) call(ctx context.Context, method string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return apperr.Wrap(err, "telegram: encode %s", method)
	}

	url := c.baseURL + "/bot" + c.token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return apperr.Wrap(err, "telegram: build %s request", method)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s request failed", method)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded because a reply is small and a compromised or confused endpoint
	// should not be able to hand back an unbounded body.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("telegram: read %s response", method)
	}

	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("telegram: %s returned status %d", method, resp.StatusCode)
	}
	if !envelope.OK {
		// The description is Telegram's own text ("chat not found", "bot was
		// blocked by the user") and carries nothing secret.
		return fmt.Errorf("telegram: %s refused: %s", method, envelope.Description)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return apperr.Wrap(err, "telegram: decode %s result", method)
	}
	return nil
}

func inlineKeyboard(options []messaging.Option) map[string]any {
	row := make([]map[string]string, 0, len(options))
	for _, opt := range options {
		row = append(row, map[string]string{
			"text":          opt.Label,
			"callback_data": opt.Value,
		})
	}
	// One row: two answers side by side read as a choice, where stacked they
	// read as a list.
	return map[string]any{"inline_keyboard": [][]map[string]string{row}}
}

// splitMessage cuts text into pieces Telegram will accept, preferring to break
// at a blank line, then a newline, then a space, and only splitting mid-word
// when a single word is somehow longer than the limit.
func splitMessage(text string, limit int) []string {
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}

	var parts []string
	for len(runes) > limit {
		cut := breakPoint(runes, limit)
		parts = append(parts, string(runes[:cut]))
		for cut < len(runes) && runes[cut] == '\n' {
			cut++
		}
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		parts = append(parts, string(runes))
	}
	return parts
}

func breakPoint(runes []rune, limit int) int {
	for _, sep := range []string{"\n\n", "\n", " "} {
		if at := lastIndexRune(runes[:limit], sep); at > limit/2 {
			return at
		}
	}
	return limit
}

func lastIndexRune(runes []rune, sep string) int {
	s := []rune(sep)
	for i := len(runes) - len(s); i >= 0; i-- {
		matched := true
		for j := range s {
			if runes[i+j] != s[j] {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

// chatIDFromString is the inverse of chatID: the messaging package stores an
// external id as text, and the Bot API wants the number back.
//
// The error stays vague on purpose. A malformed id means North's own row is
// wrong, which is worth a log line but not worth echoing an identifier into
// one.
func chatIDFromString(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errors.New("telegram: stored chat id is not a number")
	}
	return id, nil
}

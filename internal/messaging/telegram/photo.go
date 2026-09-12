package telegram

import (
	"bytes"
	"context"
	"mime/multipart"
	"strconv"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// captionLimit is what Telegram accepts on a photo. A longer caption is
// refused outright, which would lose the picture along with the words.
const captionLimit = 1024

// photoFilename is what the upload is named. Telegram shows it nowhere; it
// exists because a multipart file part without one is not a file part.
const photoFilename = "digest.png"

// sendPhoto uploads an image to a chat.
//
// Multipart rather than a URL, which is what sendAnimation uses. That path
// works because Telegram fetches the URL itself — fine for a public
// illustration, wrong for a card describing somebody's sleep, which would have
// to be readable by anyone holding the link for as long as it lived.
func (c *Client) sendPhoto(ctx context.Context, chatID int64, photo []byte, caption string) error {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)

	if err := form.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return apperr.Wrap(err, "telegram: build sendPhoto form")
	}
	if caption != "" {
		if err := form.WriteField("caption", truncateRunes(caption, captionLimit)); err != nil {
			return apperr.Wrap(err, "telegram: build sendPhoto form")
		}
	}

	part, err := form.CreateFormFile("photo", photoFilename)
	if err != nil {
		return apperr.Wrap(err, "telegram: build sendPhoto form")
	}
	if _, err := part.Write(photo); err != nil {
		return apperr.Wrap(err, "telegram: write photo")
	}
	if err := form.Close(); err != nil {
		return apperr.Wrap(err, "telegram: close sendPhoto form")
	}

	return c.do(ctx, "sendPhoto", form.FormDataContentType(), &buf, nil)
}

// truncateRunes cuts to a rune count rather than a byte count, so a caption
// ending in an accented character is not cut through the middle of it.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

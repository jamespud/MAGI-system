package a2aapp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrInvalidCursor = errors.New("invalid a2a cursor")

// CursorCodec encodes an opaque keyset cursor for the stable task ordering.
type CursorCodec struct {
	MaxPageSize int
}

type cursorPayload struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
}

func (c CursorCodec) Encode(cursor TaskCursor) (string, error) {
	if cursor.ID == "" || cursor.CreatedAt.IsZero() {
		return "", fmt.Errorf("%w: cursor requires timestamp and id", ErrInvalidCursor)
	}
	payload, err := json.Marshal(cursorPayload{
		Version: 1, CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano), ID: cursor.ID,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func (c CursorCodec) Decode(encoded string) (TaskCursor, error) {
	if encoded == "" || strings.Contains(encoded, "=") {
		return TaskCursor{}, fmt.Errorf("%w: malformed base64url", ErrInvalidCursor)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return TaskCursor{}, fmt.Errorf("%w: malformed base64url: %v", ErrInvalidCursor, err)
	}
	var payload cursorPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return TaskCursor{}, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidCursor, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return TaskCursor{}, fmt.Errorf("%w: trailing JSON", ErrInvalidCursor)
	}
	if payload.Version != 1 || strings.TrimSpace(payload.ID) == "" || !strings.HasSuffix(payload.CreatedAt, "Z") {
		return TaskCursor{}, fmt.Errorf("%w: unsupported cursor payload", ErrInvalidCursor)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || createdAt.Location() != time.UTC {
		return TaskCursor{}, fmt.Errorf("%w: timestamp must be RFC3339 UTC", ErrInvalidCursor)
	}
	return TaskCursor{CreatedAt: createdAt, ID: payload.ID}, nil
}

// PageSize applies the public default and configured upper bound. Invalid or
// absent values use the default; callers needing a hard error can use
// ValidatePageSize.
func (c CursorCodec) PageSize(size int) int {
	if size <= 0 {
		size = 50
	}
	max := c.MaxPageSize
	if max <= 0 {
		max = 100
	}
	if size > max {
		return max
	}
	return size
}

func (c CursorCodec) ValidatePageSize(size int) (int, error) {
	if size < 0 {
		return 0, fmt.Errorf("%w: page size must not be negative", ErrInvalidInput)
	}
	if size == 0 {
		return c.PageSize(0), nil
	}
	max := c.MaxPageSize
	if max <= 0 {
		max = 100
	}
	if size > max {
		return 0, fmt.Errorf("%w: page size exceeds %d", ErrInvalidInput, max)
	}
	return size, nil
}

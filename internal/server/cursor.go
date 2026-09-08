package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ZoOwen/loker-id/internal/store"
)

// cursorPayload is the JSON shape base64-encoded into the opaque
// next_cursor/cursor string clients pass back — opaque to them, but
// simple enough to encode/decode without a binary format.
type cursorPayload struct {
	PostedAt *time.Time `json:"posted_at,omitempty"`
	ID       string     `json:"id"`
}

func encodeCursor(c *store.Cursor) *string {
	if c == nil {
		return nil
	}
	b, err := json.Marshal(cursorPayload{PostedAt: c.PostedAt, ID: c.ID.String()})
	if err != nil {
		return nil
	}
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}

func decodeCursor(s string) (*store.Cursor, error) {
	if s == "" {
		return nil, nil
	}

	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("malformed cursor")
	}

	var payload cursorPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, fmt.Errorf("malformed cursor")
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, fmt.Errorf("malformed cursor")
	}

	return &store.Cursor{PostedAt: payload.PostedAt, ID: id}, nil
}

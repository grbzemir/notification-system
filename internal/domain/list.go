package domain

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

var ErrInvalidCursor = errors.New("invalid cursor")

// Cursor listede "nerede kaldık" bilgisidir: son görülen kaydın (created_at, id) çifti.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ListFilter listeleme sorgusunun filtreleri ve sayfalama bilgisidir.
type ListFilter struct {
	Status  *Status
	Channel *Channel
	BatchID *uuid.UUID
	From    *time.Time // created_at >= From
	To      *time.Time // created_at <= To
	Limit   int
	Cursor  *Cursor
}

// Page bir sayfa sonucudur. NextCursor boşsa son sayfadayız.
type Page struct {
	Items      []*Notification `json:"data"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// EncodeCursor imleci client'a verilecek opak bir metne çevirir.
func EncodeCursor(c Cursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor EncodeCursor'ın tersidir. Bozuk metinde ErrInvalidCursor döner.
func DecodeCursor(s string) (*Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	return &Cursor{CreatedAt: t, ID: u}, nil
}

package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	in := Cursor{CreatedAt: time.Date(2026, 11, 19, 14, 30, 5, 123456000, time.UTC), ID: uuid.New()}
	out, err := DecodeCursor(EncodeCursor(in))
	if err != nil {
		t.Fatal(err)
	}
	if !out.CreatedAt.Equal(in.CreatedAt) || out.ID != in.ID {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "###", "bm90LWEtY3Vyc29y", "MjAyNi0wMS0wMXxub3QtYS11dWlk"} {
		if _, err := DecodeCursor(s); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("%q: want ErrInvalidCursor, got %v", s, err)
		}
	}
}

func TestValidateBatch(t *testing.T) {
	ok := NewNotification{Recipient: "+905551234567", Channel: ChannelSMS, Content: "hi"}

	if err := ValidateBatch(nil); err == nil {
		t.Fatal("empty batch must fail")
	}
	if err := ValidateBatch(make([]NewNotification, MaxBatchSize+1)); err == nil {
		t.Fatal("oversized batch must fail")
	}

	items := make([]NewNotification, MaxBatchSize)
	for i := range items {
		items[i] = ok
	}
	if err := ValidateBatch(items); err != nil {
		t.Fatalf("1000 valid items must pass: %v", err)
	}
	if items[0].Priority != PriorityNormal {
		t.Fatal("default priority should be applied in place")
	}

	items[2].Channel = "fax"
	items[5].Content = ""
	var verr *ValidationError
	if err := ValidateBatch(items); !errors.As(err, &verr) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	var fields []string
	for _, e := range verr.Errors {
		fields = append(fields, e.Field)
	}
	joined := strings.Join(fields, ",")
	if !strings.Contains(joined, "notifications[2].channel") || !strings.Contains(joined, "notifications[5].content") {
		t.Fatalf("fields = %v", fields)
	}
}

func TestValidateBatchCapsReportedErrors(t *testing.T) {
	items := make([]NewNotification, MaxBatchSize) // hepsi geçersiz
	var verr *ValidationError
	if err := ValidateBatch(items); !errors.As(err, &verr) {
		t.Fatal("want validation error")
	}
	if len(verr.Errors) > maxReportedErrors+4 {
		t.Fatalf("too many reported errors: %d", len(verr.Errors))
	}
}

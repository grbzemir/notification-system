package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewNotificationValidate(t *testing.T) {
	tests := []struct {
		name       string
		in         NewNotification
		wantFields []string // beklenen hatalı alanlar, boşsa geçerli
	}{
		{"valid sms", NewNotification{"+905551234567", ChannelSMS, "hello", PriorityHigh}, nil},
		{"valid email", NewNotification{"user@example.com", ChannelEmail, "hello", ""}, nil},
		{"valid push", NewNotification{"device-token-abc", ChannelPush, "hello", PriorityLow}, nil},
		{"empty priority defaults to normal", NewNotification{"+905551234567", ChannelSMS, "hi", ""}, nil},
		{"invalid channel", NewNotification{"+905551234567", "fax", "hi", ""}, []string{"channel"}},
		{"missing recipient", NewNotification{"", ChannelSMS, "hi", ""}, []string{"recipient"}},
		{"missing content", NewNotification{"+905551234567", ChannelSMS, "   ", ""}, []string{"content"}},
		{"bad phone", NewNotification{"05551234567", ChannelSMS, "hi", ""}, []string{"recipient"}},
		{"bad email", NewNotification{"not-an-email", ChannelEmail, "hi", ""}, []string{"recipient"}},
		{"email with display name rejected", NewNotification{"Bob <bob@example.com>", ChannelEmail, "hi", ""}, []string{"recipient"}},
		{"sms too long", NewNotification{"+905551234567", ChannelSMS, strings.Repeat("a", 161), ""}, []string{"content"}},
		{"sms at limit", NewNotification{"+905551234567", ChannelSMS, strings.Repeat("a", 160), ""}, nil},
		{"sms limit counts characters not bytes", NewNotification{"+905551234567", ChannelSMS, strings.Repeat("ş", 160), ""}, nil},
		{"invalid priority", NewNotification{"+905551234567", ChannelSMS, "hi", "urgent"}, []string{"priority"}},
		{"multiple errors", NewNotification{"", "fax", "", "urgent"}, []string{"channel", "priority", "recipient", "content"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			err := in.Validate()
			if len(tt.wantFields) == 0 {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				if in.Priority == "" {
					t.Fatal("priority should default to normal")
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("expected *ValidationError, got %v", err)
			}
			got := map[string]bool{}
			for _, e := range verr.Errors {
				got[e.Field] = true
			}
			if len(got) != len(tt.wantFields) {
				t.Fatalf("fields = %v, want %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if !got[f] {
					t.Fatalf("missing error for field %q, got %v", f, got)
				}
			}
		})
	}
}

package domain

import (
	"testing"
	"time"
)

func TestScheduledAtValidation(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) *time.Time {
		v := now.Add(d)
		return &v
	}
	tests := []struct {
		name    string
		at      *time.Time
		wantErr bool
	}{
		{"no schedule", nil, false},
		{"one hour ahead", at(time.Hour), false},
		{"almost a year ahead", at(MaxScheduleAhead - time.Hour), false},
		{"in the past", at(-time.Minute), true},
		{"right now", at(0), true},
		{"more than a year ahead", at(MaxScheduleAhead + time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := NewNotification{Recipient: "+905551234567", Channel: ChannelSMS, Content: "hi", ScheduledAt: tt.at}
			err := in.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected a validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestScheduledAtErrorFieldName(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	in := NewNotification{Recipient: "+905551234567", Channel: ChannelSMS, Content: "hi", ScheduledAt: &past}
	verr, ok := in.Validate().(*ValidationError)
	if !ok || len(verr.Errors) != 1 || verr.Errors[0].Field != "scheduled_at" {
		t.Fatalf("want a single scheduled_at error, got %v", in.Validate())
	}
}

func TestBatchReportsScheduleErrorWithIndex(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	items := []NewNotification{
		{Recipient: "+905551234567", Channel: ChannelSMS, Content: "ok"},
		{Recipient: "+905551234567", Channel: ChannelSMS, Content: "late", ScheduledAt: &past},
	}
	verr, ok := ValidateBatch(items).(*ValidationError)
	if !ok || len(verr.Errors) != 1 || verr.Errors[0].Field != "notifications[1].scheduled_at" {
		t.Fatalf("unexpected result: %v", ValidateBatch(items))
	}
}

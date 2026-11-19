package queue

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

func TestPriorityValueOrdering(t *testing.T) {
	high, normal, low := PriorityValue(domain.PriorityHigh), PriorityValue(domain.PriorityNormal), PriorityValue(domain.PriorityLow)
	if !(high > normal && normal > low && low > 0) {
		t.Fatalf("high=%d normal=%d low=%d", high, normal, low)
	}
	if high > MaxPriority {
		t.Fatalf("high priority %d exceeds queue max %d", high, MaxPriority)
	}
	if PriorityValue("unknown") != normal {
		t.Fatal("unknown priority should map to normal")
	}
}

func TestQueueName(t *testing.T) {
	if QueueName(domain.ChannelSMS) != "notifications.sms" {
		t.Fatalf("got %s", QueueName(domain.ChannelSMS))
	}
}

func TestMessageRoundTrip(t *testing.T) {
	n := &domain.Notification{ID: uuid.New(), Channel: domain.ChannelEmail, Priority: domain.PriorityHigh, Content: "secret body"}
	body, err := NewMessage(n).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret body") {
		t.Fatalf("message must not carry content: %s", body)
	}
	got, err := DecodeMessage(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != n.ID.String() || got.Channel != domain.ChannelEmail || got.Priority != domain.PriorityHigh {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeMessageRejectsInvalid(t *testing.T) {
	good := uuid.New().String()
	cases := map[string]string{
		"not json":        `{bozuk`,
		"bad id":          `{"id":"123","channel":"sms","priority":"high"}`,
		"bad channel":     `{"id":"` + good + `","channel":"fax","priority":"high"}`,
		"bad priority":    `{"id":"` + good + `","channel":"sms","priority":"urgent"}`,
		"empty object":    `{}`,
		"channel missing": `{"id":"` + good + `"}`,
	}
	for name, body := range cases {
		if _, err := DecodeMessage([]byte(body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	m, err := DecodeMessage([]byte(`{"id":"` + good + `","channel":"push"}`))
	if err != nil || m.Priority != domain.PriorityNormal {
		t.Fatalf("missing priority should default to normal, got %+v err=%v", m, err)
	}
}

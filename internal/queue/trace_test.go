package queue

import (
	"reflect"
	"sort"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestTableCarrier(t *testing.T) {
	h := amqp.Table{"traceparent": "00-abc-def-01", "x-count": int32(3)}
	c := tableCarrier(h)

	if got := c.Get("traceparent"); got != "00-abc-def-01" {
		t.Fatalf("Get = %q", got)
	}
	if got := c.Get("x-count"); got != "" {
		t.Fatalf("non-string header must read as empty, got %q", got)
	}
	if got := c.Get("missing"); got != "" {
		t.Fatalf("missing header must read as empty, got %q", got)
	}

	c.Set("tracestate", "k=v")
	if h["tracestate"] != "k=v" {
		t.Fatal("Set must write through to the underlying table")
	}

	keys := c.Keys()
	sort.Strings(keys)
	if want := []string{"traceparent", "tracestate", "x-count"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("Keys = %v, want %v", keys, want)
	}
}

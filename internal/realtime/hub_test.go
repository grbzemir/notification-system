package realtime

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/grbzemir/notification-system/internal/events"
)

func recv(t *testing.T, c *Client) events.Event {
	t.Helper()
	select {
	case msg := <-c.Send():
		var e events.Event
		if err := json.Unmarshal(msg, &e); err != nil {
			t.Fatal(err)
		}
		return e
	case <-time.After(time.Second):
		t.Fatal("no message received")
		return events.Event{}
	}
}

func expectNone(t *testing.T, c *Client) {
	t.Helper()
	select {
	case msg := <-c.Send():
		t.Fatalf("unexpected message: %s", msg)
	default:
	}
}

func TestBroadcastOnlyReachesSubscribers(t *testing.T) {
	h := NewHub()
	a, b := NewClient(4), NewClient(4)
	h.Register(a)
	h.Register(b)
	_ = a.Subscribe([]string{"n1"})
	_ = b.Subscribe([]string{"n2"})

	h.Broadcast(events.Event{ID: "n1", Status: "sent"})

	if e := recv(t, a); e.ID != "n1" || e.Status != "sent" {
		t.Fatalf("a got %+v", e)
	}
	expectNone(t, b)
}

func TestAllReceivesEverything(t *testing.T) {
	h := NewHub()
	c := NewClient(4)
	c.SetAll(true)
	h.Register(c)

	h.Broadcast(events.Event{ID: "x", Status: "failed"})

	if e := recv(t, c); e.ID != "x" {
		t.Fatalf("got %+v", e)
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	c := NewClient(4)
	h.Register(c)
	_ = c.Subscribe([]string{"n1"})
	c.Unsubscribe([]string{"n1"})

	h.Broadcast(events.Event{ID: "n1"})

	expectNone(t, c)
}

func TestUnregisteredClientGetsNothing(t *testing.T) {
	h := NewHub()
	c := NewClient(4)
	c.SetAll(true)
	h.Register(c)
	h.Unregister(c)

	h.Broadcast(events.Event{ID: "n1"})

	expectNone(t, c)
	if h.Count() != 0 {
		t.Fatalf("count = %d", h.Count())
	}
}

// Yavaş istemci (kuyruğu dolu) atılır; diğer istemciler etkilenmez ve Broadcast bloklanmaz.
func TestSlowClientIsKickedWithoutBlockingOthers(t *testing.T) {
	h := NewHub()
	slow, fast := NewClient(1), NewClient(16)
	slow.SetAll(true)
	fast.SetAll(true)
	h.Register(slow)
	h.Register(fast)

	finished := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			h.Broadcast(events.Event{ID: "n", Status: "pending"})
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked on a slow client")
	}

	select {
	case <-slow.Done():
	default:
		t.Fatal("slow client should have been kicked")
	}
	for i := 0; i < 5; i++ {
		recv(t, fast)
	}
	select {
	case <-fast.Done():
		t.Fatal("fast client must not be kicked")
	default:
	}
}

func TestSubscriptionLimit(t *testing.T) {
	c := NewClient(1)
	ids := make([]string, MaxSubscriptions)
	for i := range ids {
		ids[i] = string(rune('a'+i%26)) + string(rune('A'+i/26))
	}
	if err := c.Subscribe(ids); err != nil {
		t.Fatal(err)
	}
	if err := c.Subscribe([]string{"one-more"}); err != ErrTooManySubscriptions {
		t.Fatalf("err = %v, want ErrTooManySubscriptions", err)
	}
	if err := c.Subscribe(ids[:3]); err != nil { // zaten abone olunanlar sayıyı artırmaz
		t.Fatalf("re-subscribing existing ids must be allowed: %v", err)
	}
}

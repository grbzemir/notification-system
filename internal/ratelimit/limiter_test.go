package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scriptedBackend struct {
	denials int
	err     error
	calls   int
	keys    []string
}

func (b *scriptedBackend) Take(_ context.Context, key string) (bool, time.Duration, error) {
	b.calls++
	b.keys = append(b.keys, key)
	if b.err != nil {
		return false, 0, b.err
	}
	if b.denials > 0 {
		b.denials--
		return false, 5 * time.Millisecond, nil
	}
	return true, 0, nil
}

func TestWaitReturnsImmediatelyWhenAllowed(t *testing.T) {
	b := &scriptedBackend{}
	if err := New(b).Wait(context.Background(), "sms"); err != nil {
		t.Fatal(err)
	}
	if b.calls != 1 || b.keys[0] != "rl:sms" {
		t.Fatalf("calls=%d keys=%v", b.calls, b.keys)
	}
}

func TestWaitRetriesUntilAllowed(t *testing.T) {
	b := &scriptedBackend{denials: 3}
	start := time.Now()
	if err := New(b).Wait(context.Background(), "email"); err != nil {
		t.Fatal(err)
	}
	if b.calls != 4 {
		t.Fatalf("calls = %d, want 4", b.calls)
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("limiter did not actually wait between attempts")
	}
}

func TestWaitStopsOnContextCancel(t *testing.T) {
	b := &scriptedBackend{denials: 1 << 30}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := New(b).Wait(ctx, "push")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitPropagatesBackendError(t *testing.T) {
	want := errors.New("redis down")
	if err := New(&scriptedBackend{err: want}).Wait(context.Background(), "sms"); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

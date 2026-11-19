package relay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grbzemir/notification-system/internal/domain"
)

type fakeSource struct {
	counts []int
	err    error
	calls  int
}

func (f *fakeSource) PublishUnqueued(_ context.Context, _ time.Duration, _ int,
	_ func(context.Context, []*domain.Notification) error) (int, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	if f.calls > len(f.counts) {
		return 0, nil
	}
	return f.counts[f.calls-1], nil
}

type nopPub struct{}

func (nopPub) Publish(context.Context, []*domain.Notification) error { return nil }

func TestDrainContinuesUntilBatchIsNotFull(t *testing.T) {
	src := &fakeSource{counts: []int{500, 500, 120}}
	New(src, nopPub{}).drain(context.Background())
	if src.calls != 3 {
		t.Fatalf("calls = %d, want 3", src.calls)
	}
}

func TestDrainStopsOnError(t *testing.T) {
	src := &fakeSource{err: errors.New("db down")}
	New(src, nopPub{}).drain(context.Background())
	if src.calls != 1 {
		t.Fatalf("calls = %d, want 1", src.calls)
	}
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	r := New(&fakeSource{}, nopPub{})
	r.interval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

// fakeRepo için varsayılan (no-op) MarkQueued. İşaretlemeyi doğrulayan testler queueRepo kullanır.
func (r *fakeRepo) MarkQueued(context.Context, []uuid.UUID) error { return nil }

type queueRepo struct {
	*fakeRepo
	mu     sync.Mutex
	queued map[uuid.UUID]bool
}

func newQueueRepo() *queueRepo {
	return &queueRepo{fakeRepo: newFakeRepo(), queued: map[uuid.UUID]bool{}}
}

func (r *queueRepo) MarkQueued(_ context.Context, ids []uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range ids {
		r.queued[id] = true
	}
	return nil
}

type fakePublisher struct {
	mu    sync.Mutex
	calls [][]*domain.Notification
	err   error
}

func (f *fakePublisher) Publish(_ context.Context, ns []*domain.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, ns)
	return nil
}

func TestCreatePublishesAndMarksQueued(t *testing.T) {
	repo, pub := newQueueRepo(), &fakePublisher{}
	svc := NewNotificationService(repo, newFakeCache(), WithPublisher(pub))

	n, created, err := svc.Create(context.Background(), validInput(), "")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if len(pub.calls) != 1 || len(pub.calls[0]) != 1 || pub.calls[0][0].ID != n.ID {
		t.Fatalf("publish calls = %+v", pub.calls)
	}
	if !repo.queued[n.ID] {
		t.Fatal("notification should be marked queued")
	}
}

func TestIdempotentReplayDoesNotPublishAgain(t *testing.T) {
	repo, pub := newQueueRepo(), &fakePublisher{}
	svc := NewNotificationService(repo, newFakeCache(), WithPublisher(pub))

	for i := 0; i < 3; i++ {
		if _, _, err := svc.Create(context.Background(), validInput(), "key-1"); err != nil {
			t.Fatal(err)
		}
	}
	if len(pub.calls) != 1 {
		t.Fatalf("published %d times, want 1", len(pub.calls))
	}
}

func TestPublishFailureStillSucceedsAndLeavesRecordUnqueued(t *testing.T) {
	repo, pub := newQueueRepo(), &fakePublisher{err: errors.New("broker down")}
	svc := NewNotificationService(repo, newFakeCache(), WithPublisher(pub))

	n, created, err := svc.Create(context.Background(), validInput(), "")
	if err != nil || !created || n == nil {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if repo.count() != 1 {
		t.Fatalf("record must be saved, count=%d", repo.count())
	}
	if len(repo.queued) != 0 {
		t.Fatal("record must NOT be marked queued when publish failed")
	}
}

func TestValidationFailureDoesNotPublish(t *testing.T) {
	pub := &fakePublisher{}
	svc := NewNotificationService(newQueueRepo(), newFakeCache(), WithPublisher(pub))
	in := validInput()
	in.Channel = "fax"
	if _, _, err := svc.Create(context.Background(), in, ""); err == nil {
		t.Fatal("expected validation error")
	}
	if len(pub.calls) != 0 {
		t.Fatal("must not publish invalid notification")
	}
}

func TestBatchPublishesEverythingInOneCall(t *testing.T) {
	repo, pub := newQueueRepo(), &fakePublisher{}
	svc := NewNotificationService(repo, newFakeCache(), WithPublisher(pub))

	items := make([]domain.NewNotification, domain.MaxBatchSize)
	for i := range items {
		items[i] = validInput()
	}
	if _, _, err := svc.CreateBatch(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	if len(pub.calls) != 1 || len(pub.calls[0]) != 1000 {
		t.Fatalf("want 1 call with 1000 notifications, got %d calls", len(pub.calls))
	}
	if len(repo.queued) != 1000 {
		t.Fatalf("queued = %d, want 1000", len(repo.queued))
	}
}

func TestInvalidBatchDoesNotPublish(t *testing.T) {
	pub := &fakePublisher{}
	svc := NewNotificationService(newQueueRepo(), newFakeCache(), WithPublisher(pub))
	if _, _, err := svc.CreateBatch(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
	if len(pub.calls) != 0 {
		t.Fatal("must not publish")
	}
}

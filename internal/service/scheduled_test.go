package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grbzemir/notification-system/internal/domain"
)

func scheduledInput(at time.Time) domain.NewNotification {
	in := validInput()
	in.ScheduledAt = &at
	return in
}

func TestScheduledNotificationIsStoredButNotPublished(t *testing.T) {
	repo, pub := newQueueRepo(), &fakePublisher{}
	svc := NewNotificationService(repo, newFakeCache(), WithPublisher(pub))

	at := time.Now().Add(time.Hour)
	n, created, err := svc.Create(context.Background(), scheduledInput(at), "")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if n.Status != domain.StatusScheduled {
		t.Fatalf("status = %s, want scheduled", n.Status)
	}
	if n.ScheduledAt == nil || !n.ScheduledAt.Equal(at) {
		t.Fatalf("scheduled_at = %v, want %v", n.ScheduledAt, at)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("a scheduled notification must not be published yet, got %d publish calls", len(pub.calls))
	}
	if repo.queued[n.ID] {
		t.Fatal("a scheduled notification must not be marked queued")
	}
}

func TestScheduledInThePastIsRejected(t *testing.T) {
	svc := NewNotificationService(newQueueRepo(), newFakeCache())
	_, _, err := svc.Create(context.Background(), scheduledInput(time.Now().Add(-time.Minute)), "")
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestIdempotentReplayOfScheduledNotification(t *testing.T) {
	svc := NewNotificationService(newQueueRepo(), newFakeCache())
	at := time.Now().Add(time.Hour)

	first, _, err := svc.Create(context.Background(), scheduledInput(at), "key-s")
	if err != nil {
		t.Fatal(err)
	}
	second, created, err := svc.Create(context.Background(), scheduledInput(at), "key-s")
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("same key and same schedule must replay: created=%v err=%v", created, err)
	}
	if _, _, err := svc.Create(context.Background(), scheduledInput(at.Add(time.Hour)), "key-s"); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same key with another schedule must conflict, got %v", err)
	}
}

func TestBatchPublishesOnlyUnscheduledItems(t *testing.T) {
	repo, pub := newQueueRepo(), &fakePublisher{}
	svc := NewNotificationService(repo, newFakeCache(), WithPublisher(pub))

	items := []domain.NewNotification{validInput(), scheduledInput(time.Now().Add(time.Hour)), validInput()}
	if _, count, err := svc.CreateBatch(context.Background(), items); err != nil || count != 3 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if len(pub.calls) != 1 || len(pub.calls[0]) != 2 {
		t.Fatalf("want one publish call with the 2 unscheduled items, got %+v", pub.calls)
	}
	for _, n := range pub.calls[0] {
		if n.Status != domain.StatusPending {
			t.Fatalf("published notification has status %s", n.Status)
		}
	}
}

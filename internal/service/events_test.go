package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/events"
)

type cancelRepo struct {
	*fakeRepo
	n   *domain.Notification
	err error
}

func (r *cancelRepo) Cancel(context.Context, uuid.UUID) (*domain.Notification, error) {
	return r.n, r.err
}

type recordingEvents struct{ got []events.Event }

func (r *recordingEvents) Publish(_ context.Context, e events.Event) { r.got = append(r.got, e) }

func TestCancelPublishesEvent(t *testing.T) {
	n := &domain.Notification{ID: uuid.New(), Status: domain.StatusCancelled}
	rec := &recordingEvents{}
	svc := NewNotificationService(&cancelRepo{fakeRepo: newFakeRepo(), n: n}, newFakeCache(), WithEvents(rec))

	if _, err := svc.Cancel(context.Background(), n.ID); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0].ID != n.ID.String() || rec.got[0].Status != "cancelled" {
		t.Fatalf("events = %+v", rec.got)
	}
}

func TestFailedCancelPublishesNothing(t *testing.T) {
	rec := &recordingEvents{}
	svc := NewNotificationService(&cancelRepo{fakeRepo: newFakeRepo(), err: domain.ErrNotCancellable}, newFakeCache(), WithEvents(rec))

	_, err := svc.Cancel(context.Background(), uuid.New())

	if !errors.Is(err, domain.ErrNotCancellable) || len(rec.got) != 0 {
		t.Fatalf("err=%v events=%v", err, rec.got)
	}
}

func TestCancelWorksWithoutEventPublisher(t *testing.T) {
	n := &domain.Notification{ID: uuid.New(), Status: domain.StatusCancelled}
	svc := NewNotificationService(&cancelRepo{fakeRepo: newFakeRepo(), n: n}, newFakeCache())
	if _, err := svc.Cancel(context.Background(), n.ID); err != nil {
		t.Fatal(err)
	}
}

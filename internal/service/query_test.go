package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

var lastListLimit int

// Aşağıdaki üç metot, fakeRepo'nun yeni repository interface'ini karşılaması içindir.

func (r *fakeRepo) CreateBatch(_ context.Context, items []*domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range items {
		cp := *n
		r.byID[n.ID] = &cp
	}
	return nil
}

func (r *fakeRepo) Cancel(_ context.Context, _ uuid.UUID) (*domain.Notification, error) {
	return nil, errors.New("not used in these tests")
}

func (r *fakeRepo) BatchSummary(_ context.Context, _ uuid.UUID) (*domain.BatchSummary, error) {
	return nil, errors.New("not used in these tests")
}

// List gerçek SQL sorgusunun davranışını taklit eder: created_at DESC, id DESC, cursor'dan sonrası, LIMIT limit+1.
func (r *fakeRepo) List(_ context.Context, f domain.ListFilter) ([]*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lastListLimit = f.Limit

	var all []*domain.Notification
	for _, n := range r.byID {
		cp := *n
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].ID.String() > all[j].ID.String()
	})

	var out []*domain.Notification
	for _, n := range all {
		if f.Cursor != nil {
			after := n.CreatedAt.Before(f.Cursor.CreatedAt) ||
				(n.CreatedAt.Equal(f.Cursor.CreatedAt) && n.ID.String() < f.Cursor.ID.String())
			if !after {
				continue
			}
		}
		out = append(out, n)
		if len(out) == f.Limit+1 {
			break
		}
	}
	return out, nil
}

func TestCreateBatchIsAllOrNothing(t *testing.T) {
	repo := newFakeRepo()
	svc := NewNotificationService(repo, newFakeCache())

	items := []domain.NewNotification{validInput(), validInput(), validInput()}
	items[1].Channel = "fax"
	_, _, err := svc.CreateBatch(context.Background(), items)
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want validation error, got %v", err)
	}
	if repo.count() != 0 {
		t.Fatalf("nothing must be saved, got %d records", repo.count())
	}
}

func TestCreateBatchSharesOneBatchID(t *testing.T) {
	repo := newFakeRepo()
	svc := NewNotificationService(repo, newFakeCache())

	items := make([]domain.NewNotification, domain.MaxBatchSize)
	for i := range items {
		items[i] = validInput()
	}
	batchID, count, err := svc.CreateBatch(context.Background(), items)
	if err != nil || count != 1000 || repo.count() != 1000 {
		t.Fatalf("count=%d saved=%d err=%v", count, repo.count(), err)
	}
	for _, n := range repo.byID {
		if n.BatchID == nil || *n.BatchID != batchID || n.Status != domain.StatusPending {
			t.Fatalf("bad record: %+v", n)
		}
	}

	if _, _, err := svc.CreateBatch(context.Background(), append(items, validInput())); err == nil {
		t.Fatal("1001 items must be rejected")
	}
}

func seedNotifications(repo *fakeRepo, n int) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		id := uuid.New()
		repo.byID[id] = &domain.Notification{ID: id, CreatedAt: base.Add(time.Duration(i) * time.Minute), Status: domain.StatusPending}
	}
}

func TestListPaginatesWithoutGapsOrDuplicates(t *testing.T) {
	repo := newFakeRepo()
	seedNotifications(repo, 5)
	svc := NewNotificationService(repo, newFakeCache())

	seen := map[uuid.UUID]bool{}
	var prev time.Time
	var cursor *domain.Cursor
	pages := 0
	for {
		page, err := svc.List(context.Background(), domain.ListFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, n := range page.Items {
			if seen[n.ID] {
				t.Fatalf("duplicate %v", n.ID)
			}
			seen[n.ID] = true
			if !prev.IsZero() && n.CreatedAt.After(prev) {
				t.Fatal("not sorted newest first")
			}
			prev = n.CreatedAt
		}
		if page.NextCursor == "" {
			if len(page.Items) != 1 {
				t.Fatalf("last page has %d items, want 1", len(page.Items))
			}
			break
		}
		if len(page.Items) != 2 {
			t.Fatalf("full page has %d items", len(page.Items))
		}
		var err2 error
		if cursor, err2 = domain.DecodeCursor(page.NextCursor); err2 != nil {
			t.Fatal(err2)
		}
	}
	if len(seen) != 5 || pages != 3 {
		t.Fatalf("seen=%d pages=%d", len(seen), pages)
	}
}

func TestListExactMultipleOfPageSizeHasNoPhantomPage(t *testing.T) {
	repo := newFakeRepo()
	seedNotifications(repo, 4)
	svc := NewNotificationService(repo, newFakeCache())

	page, _ := svc.List(context.Background(), domain.ListFilter{Limit: 2})
	cur, _ := domain.DecodeCursor(page.NextCursor)
	page2, _ := svc.List(context.Background(), domain.ListFilter{Limit: 2, Cursor: cur})
	if len(page2.Items) != 2 || page2.NextCursor != "" {
		t.Fatalf("items=%d next=%q", len(page2.Items), page2.NextCursor)
	}
}

func TestListLimitIsClampedAndEmptyIsNotNull(t *testing.T) {
	repo := newFakeRepo()
	svc := NewNotificationService(repo, newFakeCache())

	page, _ := svc.List(context.Background(), domain.ListFilter{Limit: 100000})
	if lastListLimit != domain.MaxPageSize {
		t.Fatalf("limit = %d, want %d", lastListLimit, domain.MaxPageSize)
	}
	if page.Items == nil || len(page.Items) != 0 {
		t.Fatal("empty list must be [] not nil")
	}
	_, _ = svc.List(context.Background(), domain.ListFilter{})
	if lastListLimit != domain.DefaultPageSize {
		t.Fatalf("default limit = %d", lastListLimit)
	}
}

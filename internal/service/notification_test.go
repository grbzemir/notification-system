package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

type fakeRepo struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*domain.Notification
	byKey map[string]uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]*domain.Notification{}, byKey: map[string]uuid.UUID{}}
}

func (r *fakeRepo) Create(_ context.Context, n *domain.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n.IdempotencyKey != nil {
		if _, ok := r.byKey[*n.IdempotencyKey]; ok {
			return domain.ErrDuplicateKey // unique index davranışı
		}
		r.byKey[*n.IdempotencyKey] = n.ID
	}
	cp := *n
	r.byID[n.ID] = &cp
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *n
	return &cp, nil
}

func (r *fakeRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error) {
	r.mu.Lock()
	id, ok := r.byKey[key]
	r.mu.Unlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	return r.GetByID(ctx, id)
}

func (r *fakeRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

type fakeCache struct {
	mu      sync.Mutex
	data    map[string]uuid.UUID
	failing bool
}

func newFakeCache() *fakeCache { return &fakeCache{data: map[string]uuid.UUID{}} }

func (c *fakeCache) Get(_ context.Context, key string) (uuid.UUID, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failing {
		return uuid.Nil, false, errors.New("redis down")
	}
	id, ok := c.data[key]
	return id, ok, nil
}

func (c *fakeCache) Set(_ context.Context, key string, id uuid.UUID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failing {
		return errors.New("redis down")
	}
	c.data[key] = id
	return nil
}

func validInput() domain.NewNotification {
	return domain.NewNotification{Recipient: "+905551234567", Channel: domain.ChannelSMS, Content: "hello", Priority: domain.PriorityNormal}
}

func TestCreateWithoutKeyAlwaysInserts(t *testing.T) {
	repo := newFakeRepo()
	svc := NewNotificationService(repo, newFakeCache())
	for i := 0; i < 2; i++ {
		_, created, err := svc.Create(context.Background(), validInput(), "")
		if err != nil || !created {
			t.Fatalf("created=%v err=%v", created, err)
		}
	}
	if repo.count() != 2 {
		t.Fatalf("count = %d, want 2", repo.count())
	}
}

func TestCreateValidationFailure(t *testing.T) {
	svc := NewNotificationService(newFakeRepo(), newFakeCache())
	in := validInput()
	in.Channel = "fax"
	_, _, err := svc.Create(context.Background(), in, "k1")
	var verr *domain.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestSameKeyReturnsExistingRecord(t *testing.T) {
	repo := newFakeRepo()
	svc := NewNotificationService(repo, newFakeCache())
	first, created1, err := svc.Create(context.Background(), validInput(), "key-1")
	if err != nil || !created1 {
		t.Fatalf("first: created=%v err=%v", created1, err)
	}
	second, created2, err := svc.Create(context.Background(), validInput(), "key-1")
	if err != nil || created2 {
		t.Fatalf("second: created=%v err=%v", created2, err)
	}
	if first.ID != second.ID || repo.count() != 1 {
		t.Fatalf("ids %v/%v count=%d", first.ID, second.ID, repo.count())
	}
}

func TestSameKeyDifferentPayloadConflicts(t *testing.T) {
	svc := NewNotificationService(newFakeRepo(), newFakeCache())
	if _, _, err := svc.Create(context.Background(), validInput(), "key-1"); err != nil {
		t.Fatal(err)
	}
	other := validInput()
	other.Content = "different"
	_, _, err := svc.Create(context.Background(), other, "key-1")
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestCacheMissFallsBackToDatabaseUniqueIndex(t *testing.T) {
	repo, cache := newFakeRepo(), newFakeCache()
	svc := NewNotificationService(repo, cache)
	first, _, _ := svc.Create(context.Background(), validInput(), "key-1")

	cache.data = map[string]uuid.UUID{} // Redis'in verisi kaybolmuş gibi
	second, created, err := svc.Create(context.Background(), validInput(), "key-1")
	if err != nil || created || second.ID != first.ID || repo.count() != 1 {
		t.Fatalf("created=%v err=%v count=%d", created, err, repo.count())
	}
	if _, ok := cache.data["key-1"]; !ok {
		t.Fatal("cache should be repopulated")
	}
}

func TestCacheFailureDoesNotBreakIdempotency(t *testing.T) {
	repo, cache := newFakeRepo(), newFakeCache()
	cache.failing = true
	svc := NewNotificationService(repo, cache)
	first, c1, err1 := svc.Create(context.Background(), validInput(), "key-1")
	second, c2, err2 := svc.Create(context.Background(), validInput(), "key-1")
	if err1 != nil || err2 != nil || !c1 || c2 || first.ID != second.ID || repo.count() != 1 {
		t.Fatalf("err1=%v err2=%v c1=%v c2=%v count=%d", err1, err2, c1, c2, repo.count())
	}
}

func TestStaleCacheEntryIsIgnored(t *testing.T) {
	repo, cache := newFakeRepo(), newFakeCache()
	cache.data["key-1"] = uuid.New() // veritabanında olmayan bir ID
	svc := NewNotificationService(repo, cache)
	n, created, err := svc.Create(context.Background(), validInput(), "key-1")
	if err != nil || !created || n == nil || repo.count() != 1 {
		t.Fatalf("created=%v err=%v", created, err)
	}
}

func TestConcurrentRequestsWithSameKeyCreateOneRecord(t *testing.T) {
	repo := newFakeRepo()
	svc := NewNotificationService(repo, newFakeCache())

	const workers = 50
	var createdCount atomic.Int32
	ids := make(chan uuid.UUID, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, created, err := svc.Create(context.Background(), validInput(), "same-key")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if created {
				createdCount.Add(1)
			}
			ids <- n.ID
		}()
	}
	wg.Wait()
	close(ids)

	if createdCount.Load() != 1 {
		t.Fatalf("created %d times, want exactly 1", createdCount.Load())
	}
	if repo.count() != 1 {
		t.Fatalf("records = %d, want 1", repo.count())
	}
	var first uuid.UUID
	for id := range ids {
		if first == uuid.Nil {
			first = id
		} else if id != first {
			t.Fatalf("different IDs returned: %v vs %v", first, id)
		}
	}
}

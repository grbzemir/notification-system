package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// IdempotencyStore idempotency key -> bildirim ID eşlemesini Redis'te tutar.
type IdempotencyStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewIdempotencyStore(rdb *redis.Client, ttl time.Duration) *IdempotencyStore {
	return &IdempotencyStore{rdb: rdb, ttl: ttl}
}

func redisKey(key string) string { return "idem:" + key }

// Get key kayıtlıysa bildirim ID'sini döner. Kayıt yoksa found=false olur, hata dönmez.
func (s *IdempotencyStore) Get(ctx context.Context, key string) (uuid.UUID, bool, error) {
	val, err := s.rdb.Get(ctx, redisKey(key)).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, false, nil // bozuk değer: cache miss say
	}
	return id, true, nil
}

// Set eşlemeyi TTL ile yazar. TTL sayesinde Redis eski key'lerle dolmaz.
func (s *IdempotencyStore) Set(ctx context.Context, key string, id uuid.UUID) error {
	return s.rdb.Set(ctx, redisKey(key), id.String(), s.ttl).Err()
}

package ratelimit

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed bucket.lua
var bucketScript string

// RedisBackend token bucket'ı Redis üzerinde Lua betiğiyle atomik olarak çalıştırır.
type RedisBackend struct {
	rdb    *redis.Client
	script *redis.Script
	rate   int
	burst  int
}

// NewRedisBackend saniyede ratePerSec token üretir. Kova kapasitesi rate/10'dur (en az 1):
// boşta kalmış bir kanalın aniden büyük bir yığın göndermesini engeller.
func NewRedisBackend(rdb *redis.Client, ratePerSec int) *RedisBackend {
	burst := ratePerSec / 10
	if burst < 1 {
		burst = 1
	}
	return &RedisBackend{rdb: rdb, script: redis.NewScript(bucketScript), rate: ratePerSec, burst: burst}
}

func (b *RedisBackend) Take(ctx context.Context, key string) (bool, time.Duration, error) {
	res, err := b.script.Run(ctx, b.rdb, []string{key}, b.rate, b.burst).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("rate limit script: %w", err)
	}
	if len(res) != 2 {
		return false, 0, fmt.Errorf("rate limit script: unexpected result %v", res)
	}
	return res[0] == 1, time.Duration(res[1]) * time.Millisecond, nil
}

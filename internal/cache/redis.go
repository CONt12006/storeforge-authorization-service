package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/storeforge/authorization-service/internal/domain"
)

type RedisAccessCache struct {
	client *redis.Client
}

func NewRedisAccessCache(address, password string, database int) *RedisAccessCache {
	return &RedisAccessCache{client: redis.NewClient(&redis.Options{Addr: address, Password: password, DB: database})}
}

func (c *RedisAccessCache) key(userID int64) string {
	return fmt.Sprintf("authorization:access:%d", userID)
}

func (c *RedisAccessCache) Get(ctx context.Context, userID int64) (domain.EffectiveAccess, bool, error) {
	value, err := c.client.Get(ctx, c.key(userID)).Result()
	if err == redis.Nil {
		return domain.EffectiveAccess{}, false, nil
	}
	if err != nil {
		return domain.EffectiveAccess{}, false, err
	}
	var access domain.EffectiveAccess
	if err := json.Unmarshal([]byte(value), &access); err != nil {
		return domain.EffectiveAccess{}, false, err
	}
	return access, true, nil
}

func (c *RedisAccessCache) Set(ctx context.Context, userID int64, access domain.EffectiveAccess, ttl time.Duration) error {
	data, err := json.Marshal(access)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.key(userID), data, ttl).Err()
}

func (c *RedisAccessCache) Invalidate(ctx context.Context, userID int64) error {
	return c.client.Del(ctx, c.key(userID)).Err()
}

func (c *RedisAccessCache) InvalidateAll(ctx context.Context) error {
	var cursor uint64
	for {
		keys, next, err := c.client.Scan(ctx, cursor, "authorization:access:*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (c *RedisAccessCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *RedisAccessCache) Close() error {
	return c.client.Close()
}

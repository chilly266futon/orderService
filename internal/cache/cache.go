package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type Cache interface {
	Get(ctx context.Context, key string) (string, error) // (value, found)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

type RedisCache struct {
	client *redis.Client
	logger *zap.Logger
}

func NewRedisCache(client *redis.Client, logger *zap.Logger) *RedisCache {
	return &RedisCache{
		client: client,
		logger: logger,
	}
}

func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	if err != nil {
		c.logger.Warn("redis get failed", zap.String("key", key), zap.Error(err))
		return "", err
	}

	return val, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := c.client.Set(ctx, key, value, ttl).Err(); err != nil {
		c.logger.Warn("redis set failed", zap.String("key", key), zap.Error(err))
		return err
	}

	return nil
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, key).Err(); err != nil {
		c.logger.Warn("redis delete failed", zap.String("key", key), zap.Error(err))
		return err
	}

	return nil
}

package cache

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	cacheAccessGranted = "granted"
	cacheAccessDenied  = "denied"
)

type MarketAccessCache struct {
	cache  Cache
	logger *zap.Logger
	ttl    time.Duration
}

func NewMarketAccessCache(cache Cache, logger *zap.Logger, ttl time.Duration) *MarketAccessCache {
	return &MarketAccessCache{
		cache:  cache,
		logger: logger,
		ttl:    ttl,
	}
}

func (c *MarketAccessCache) IsMarketAccessible(ctx context.Context, marketID string, userRoles []string) (bool, error) {
	key := marketAccessCacheKey(marketID, userRoles)

	val, err := c.cache.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		c.logger.Debug("market access cache miss", zap.String("marketID", marketID))
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}

	return val == cacheAccessGranted, nil
}

func (c *MarketAccessCache) CacheMarketAccessibility(ctx context.Context, marketID string, userRoles []string, accessible bool) error {
	key := marketAccessCacheKey(marketID, userRoles)

	val := cacheAccessDenied
	if accessible {
		val = cacheAccessGranted
	}

	return c.cache.Set(ctx, key, val, c.ttl)
}

func marketAccessCacheKey(marketID string, userRoles []string) string {
	roles := make([]string, len(userRoles))
	copy(roles, userRoles)
	sort.Strings(roles)

	return makeCacheKey(marketID, roles)
}

func makeCacheKey(marketID string, roles []string) string {
	joinedRoles := strings.Join(roles, ",")
	return "market:access:" + marketID + ":" + joinedRoles
}

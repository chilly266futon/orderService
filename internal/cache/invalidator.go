package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Invalidator обеспечивает инвалидацию кэша доступа к рынкам на основе событий Redis Pub/Sub.
// Он подписывается на канал "market:updated" и при получении события удаляет все ключи кэша,
// соответствующие обновлённому рынку (паттерн "market:access:{marketID}:*").
// Реализует автоматическое переподключение с экспоненциальной задержкой при разрыве соединения.

type Invalidator struct {
	client *redis.Client
	logger *zap.Logger
}

func NewInvalidator(client *redis.Client, logger *zap.Logger) *Invalidator {
	return &Invalidator{
		client: client,
		logger: logger,
	}
}

func (i *Invalidator) Start(ctx context.Context) {
	const (
		channelName        = "market:updated"
		baseReconnectDelay = 100 * time.Millisecond
		maxReconnectDelay  = 30 * time.Second
	)

	var reconnectDelay time.Duration

	for {
		pubsub := i.client.Subscribe(ctx, channelName)
		ch := pubsub.Channel()

		i.logger.Info("subscribed to cache invalidation channel", zap.String("channel", channelName))

		// Сброс задержки переподключения после успешного подключения
		reconnectDelay = 0

	inner:
		for {
			select {
			case <-ctx.Done():
				if err := pubsub.Close(); err != nil {
					i.logger.Warn("failed to close pubsub", zap.Error(err))
				}
				i.logger.Info("cache invalidator stopped")
				return
			case msg, ok := <-ch:
				if !ok {
					// Канал закрыт, нужно переподключиться
					if err := pubsub.Close(); err != nil {
						i.logger.Warn("failed to close pubsub", zap.Error(err))
					}
					i.logger.Warn("pubsub channel closed, reconnecting...")
					break inner
				}

				marketID := msg.Payload
				pattern := "market:access:" + marketID + ":*"

				// Ограничиваем время операции инвалидации
				invalidationCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()

				start := time.Now()
				var cursor uint64
				var keys []string
				var err error
				const scanCount = 1000
				for {
					var scanKeys []string
					scanKeys, cursor, err = i.client.Scan(invalidationCtx, cursor, pattern, scanCount).Result()
					if err != nil {
						i.logger.Warn("failed to scan keys for invalidation", zap.String("market_id", marketID), zap.Error(err))
						break
					}
					keys = append(keys, scanKeys...)
					if cursor == 0 {
						break
					}
				}
				scanDuration := time.Since(start)

				if len(keys) > 0 {
					delStart := time.Now()
					if err := i.client.Del(ctx, keys...).Err(); err != nil {
						i.logger.Warn("failed to delete keys for invalidation", zap.String("market_id", marketID), zap.Error(err))
					} else {
						delDuration := time.Since(delStart)
						i.logger.Info("invalidated market access cache",
							zap.String("market_id", marketID),
							zap.Int("keys_deleted", len(keys)),
							zap.Duration("scan_duration", scanDuration),
							zap.Duration("delete_duration", delDuration),
						)
					}
				} else {
					i.logger.Debug("no keys to invalidate",
						zap.String("market_id", marketID),
						zap.Duration("scan_duration", scanDuration),
					)
				}
			}
		}

		// Закрываем pubsub перед переподключением
		if err := pubsub.Close(); err != nil {
			i.logger.Warn("failed to close pubsub during reconnect", zap.Error(err))
		}

		// Экспоненциальная задержка перед переподключением
		if reconnectDelay == 0 {
			reconnectDelay = baseReconnectDelay
		} else {
			reconnectDelay *= 2
			if reconnectDelay > maxReconnectDelay {
				reconnectDelay = maxReconnectDelay
			}
		}

		i.logger.Info("waiting before reconnect", zap.Duration("delay", reconnectDelay))
		select {
		case <-ctx.Done():
			i.logger.Info("cache invalidator stopped during reconnect")
			return
		case <-time.After(reconnectDelay):
			// продолжить внешний цикл
		}
	}
}

package clients

import (
	"context"
	"fmt"
	"github.com/chilly266futon/spotService/pkg/shared/interceptors"
	"time"

	"go.uber.org/zap"

	spotv1 "github.com/chilly266futon/spotService/gen/pb"
	"github.com/chilly266futon/spotService/pkg/shared/breaker"
	"github.com/chilly266futon/spotService/pkg/spotclient"
)

type SpotClient interface {
	MarketExists(ctx context.Context, marketID string, userRoles []spotv1.UserRole) (bool, error)
	Close() error
}

type spotClientImpl struct {
	client  *spotclient.Client
	breaker *breaker.Wrapper
	timeout time.Duration
	logger  *zap.Logger
}

type Config struct {
	Address       string
	Timeout       time.Duration
	EnableBreaker bool
	BreakerConfig breaker.Config
}

func NewSpotClient(cfg Config, logger *zap.Logger) (SpotClient, error) {
	client, err := spotclient.New(spotclient.Config{
		Address: cfg.Address,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create spot client: %w", err)
	}

	impl := &spotClientImpl{
		client:  client,
		timeout: cfg.Timeout,
		logger:  logger,
	}

	if cfg.EnableBreaker {
		impl.breaker = breaker.NewWrapper("spot-service", cfg.BreakerConfig)
	}

	return impl, nil
}

func (c *spotClientImpl) MarketExists(ctx context.Context, marketID string, userRoles []spotv1.UserRole) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	traceID := interceptors.GetTraceID(ctx)

	checkMarket := func() (bool, error) {
		markets, err := c.client.ViewMarkets(ctx, userRoles)
		if err != nil {
			c.logger.Error("market unavailable",
				zap.String("trace_id", traceID))
			return false, err
		}

		for _, market := range markets {
			if market.Id == marketID {
				return true, nil
			}
		}
		return false, nil
	}

	if c.breaker != nil {
		var exists bool
		err := c.breaker.Execute(func() error {
			var execErr error
			exists, execErr = checkMarket()
			return execErr
		})
		return exists, err
	}

	return checkMarket()
}

func (c *spotClientImpl) Close() error {
	return c.client.Close()
}

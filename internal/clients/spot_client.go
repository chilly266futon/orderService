package clients

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/sethvargo/go-retry"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	spotpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/spot"
	"github.com/chilly266futon/exchange-shared/pkg/breaker"
	"github.com/chilly266futon/exchange-shared/pkg/config"
	"github.com/chilly266futon/exchange-shared/pkg/grpcutil"
	"github.com/chilly266futon/exchange-shared/pkg/interceptors"
	"github.com/chilly266futon/exchange-shared/pkg/metrics"
	orderconfig "github.com/chilly266futon/orderService/internal/config"
)

type SpotClient interface {
	MarketExists(ctx context.Context, marketID string, userRoles []spotpb.UserRole) (bool, error)
	Close() error
}

type spotClientImpl struct {
	conn    *grpc.ClientConn
	client  spotpb.SpotInstrumentServiceClient
	breaker *breaker.Wrapper
	metrics *metrics.Metrics
	timeout time.Duration
	logger  *zap.Logger
}

func NewSpotClient(
	cfg orderconfig.SpotClient,
	cbCfg config.CircuitBreaker,
	metrics *metrics.Metrics,
	logger *zap.Logger,
) (SpotClient, error) {
	// Keepalive client parameters
	keepaliveParams := keepalive.ClientParameters{
		Time:                30 * time.Second, // send pings every 30 seconds if there is no activity
		Timeout:             10 * time.Second, // wait 10 seconds for ping ack before considering the connection dead
		PermitWithoutStream: true,             // send pings even without active streams
	}

	var transportCreds credentials.TransportCredentials
	if cfg.TLSEnabled {
		// Загружаем CA сертификат
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to add CA certificate to pool")
		}
		tlsConfig := &tls.Config{
			RootCAs: pool,
		}
		transportCreds = credentials.NewTLS(tlsConfig)
		logger.Info("TLS enabled for spot client", zap.String("ca_file", cfg.CAFile))
	} else {
		transportCreds = insecure.NewCredentials()
		logger.Info("TLS disabled for spot client (insecure)")
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(transportCreds),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithUnaryInterceptor(interceptors.TraceIDClientInterceptor()),
		grpc.WithKeepaliveParams(keepaliveParams),
	}

	var breakerWrapper *breaker.Wrapper
	if cbCfg.Enabled {
		breakerWrapper = breaker.NewWrapper("spot-client", cbCfg)
		opts = append(opts, grpc.WithUnaryInterceptor(breaker.UnaryClientInterceptor(cbCfg, metrics)))
		logger.Info("circuit breaker ENABLED for spot client",
			zap.String("address", cfg.Addr),
			zap.Uint32("attempts", cbCfg.Attempts),
			zap.Duration("timeout", cfg.Timeout),
		)
	} else {
		logger.Info("circuit breaker DISABLED for spot client", zap.String("address", cfg.Addr))
	}

	conn, err := grpcutil.NewGRPCClient(cfg.Addr, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create spot client: %w", err)
	}

	impl := &spotClientImpl{
		conn:    conn,
		client:  spotpb.NewSpotInstrumentServiceClient(conn),
		breaker: breakerWrapper,
		metrics: metrics,
		timeout: cfg.Timeout,
		logger:  logger,
	}

	return impl, nil

}

func (c *spotClientImpl) MarketExists(ctx context.Context, marketID string, userRoles []spotpb.UserRole) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	traceID := interceptors.GetTraceID(ctx)

	req := &spotpb.CheckMarketRequest{
		MarketId:  marketID,
		UserRoles: userRoles,
	}

	checkMarket := func() (bool, error) {
		resp, err := c.client.CheckMarket(ctx, req)
		if err != nil {
			c.logger.Error("market check failed",
				zap.String("trace_id", traceID),
				zap.String("market_id", marketID),
				zap.Error(err),
			)
			return false, err
		}
		return resp.Accessible, nil
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

	retryPolicy := retry.NewExponential(2 * time.Second)
	retryPolicy = retry.WithMaxRetries(3, retryPolicy)

	var result bool
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		var err error
		result, err = checkMarket()
		if err != nil {
			c.logger.Warn("Retrying market check", zap.Error(err))
		}
		return err
	})
	return result, err
}

func (c *spotClientImpl) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

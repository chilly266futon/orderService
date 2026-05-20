package main

import (
	"context"
	"time"

	"buf.build/go/protovalidate"
	"github.com/chilly266futon/exchange-shared/pkg/infra"
	"github.com/chilly266futon/orderService/internal/cache"
	"go.uber.org/zap"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	orderv1 "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"

	"github.com/chilly266futon/exchange-shared/pkg/auth"
	conf "github.com/chilly266futon/exchange-shared/pkg/config"
	"github.com/chilly266futon/exchange-shared/pkg/grpcutil"
	"github.com/chilly266futon/exchange-shared/pkg/health"
	"github.com/chilly266futon/exchange-shared/pkg/interceptors"
	"github.com/chilly266futon/exchange-shared/pkg/logger"
	"github.com/chilly266futon/exchange-shared/pkg/postgres"
	"github.com/chilly266futon/exchange-shared/pkg/telemetry"

	"github.com/chilly266futon/orderService/internal/clients"
	"github.com/chilly266futon/orderService/internal/config"
	"github.com/chilly266futon/orderService/internal/service"
	"github.com/chilly266futon/orderService/internal/storage"
	transport "github.com/chilly266futon/orderService/internal/transport/grpc"
	ordermigrations "github.com/chilly266futon/orderService/migrations"
)

const serviceName = "order-service"

func main() {
	l := logger.New()
	defer func() {
		if err := l.Sync(); err != nil {
			l.Fatal("failed to sync logger", zap.Error(err))
		}
	}()

	cfg := config.Load("config.yaml", l)

	l.Info("starting order-service", zap.Int("port", cfg.Server.Port))

	// Telemetry (трассировка + Prometheus метрики)
	shutdownTelemetry, metricsHandler, err := telemetry.Setup(serviceName, l)
	if err != nil {
		l.Fatal("failed to setup telemetry", zap.Error(err))
	}
	defer shutdownTelemetry()

	// Кастомные метрики
	m, err := infra.InitMetrics(serviceName)
	if err != nil {
		l.Fatal("failed to create metrics", zap.Error(err))
	}

	// Postgres
	dbPool, err := infra.InitPostgres(cfg.Database, l)
	if err != nil {
		l.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer dbPool.Close()

	if err := postgres.RunMigrations(dbPool, ordermigrations.FS, "."); err != nil {
		l.Fatal("migrations failed", zap.Error(err))
	}

	orderStorage := storage.NewOrderStorage(dbPool)

	// Redis
	redisClient := infra.InitRedis(cfg.Redis, l)
	defer func() {
		if err := redisClient.Close(); err != nil {
			l.Error("failed to close redis client", zap.Error(err))
		}
	}()

	marketAccessCache := cache.NewMarketAccessCache(
		cache.NewRedisCache(redisClient, l),
		l,
		30*time.Second, // TTL для кэша доступа к рынкам
	)

	invalidator := cache.NewInvalidator(redisClient, l)
	go invalidator.Start(context.Background())

	spotClient, err := clients.NewSpotClient(
		cfg.SpotClient,
		conf.CircuitBreaker{
			MaxRequests:  cfg.CircuitBreaker.MaxRequests,
			Interval:     cfg.CircuitBreaker.Interval,
			Timeout:      cfg.CircuitBreaker.Timeout,
			Attempts:     cfg.CircuitBreaker.Attempts,
			RetryDelay:   100 * time.Millisecond,
			MinRequests:  10,
			FailureRatio: 0.6,
		}, m, l,
	)
	if err != nil {
		l.Fatal("failed to create spot client", zap.Error(err))
	}
	defer func() {
		if err := spotClient.Close(); err != nil {
			l.Error("failed to close spot client", zap.Error(err))
		}
	}()

	l.Info("connected to spot service", zap.String("address", cfg.SpotClient.Addr))

	useCase := service.NewOrderUseCase(
		orderStorage,
		spotClient,
		marketAccessCache,
		cfg.OrderLimits,
		m,
		l,
		nil, // activeOrdersCache
	)

	jwtValidator := auth.NewJWTValidator(cfg.JWT.Secret, l)
	validatorInstance, err := protovalidate.New()
	if err != nil {
		l.Fatal("failed to initialize validator", zap.Error(err))
	}

	interceptorsChain, rateLimiter := interceptors.NewInterceptorChain(
		l,
		m,
		cfg.RateLimit,
		cfg.Logger,
		jwtValidator,
		validatorInstance,
		[]string{"/order.v1.OrderService/CreateOrder", "/order.v1.OrderService/ListOrders"},
		cfg.OperationTimeouts,
	)
	defer rateLimiter.Stop() // Корректно завершаем клинап пользователей

	grpcServer, err := grpcutil.NewServer(
		grpcutil.ServerConfig{
			Host:            cfg.Server.Host,
			Port:            cfg.Server.Port,
			ShutdownTimeout: cfg.Server.ShutdownTimeout,
		}, l, interceptorsChain...,
	)
	if err != nil {
		l.Fatal("failed to create server", zap.Error(err))
	}

	orderv1.RegisterOrderServiceServer(grpcServer.GRPCServer(), transport.NewOrderServer(useCase))

	// health check
	if cfg.Server.HealthEnabled {
		healthServer := health.NewServer()

		// Проверка Postgres с тайм-аутом
		healthTimeout := cfg.Server.HealthCheckTimeout
		if healthTimeout == 0 {
			healthTimeout = 2 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()
		if err := health.CheckPostgresHealth(ctx, orderStorage.DB()); err == nil {
			healthServer.SetHealthy("postgres")
		} else {
			l.Warn("postgres health check failed", zap.Error(err))
		}

		// Проверка Redis с тайм-аутом
		ctx, cancel = context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()
		if err := health.CheckRedisHealth(ctx, redisClient); err == nil {
			healthServer.SetHealthy("redis")
		} else {
			l.Warn("redis health check failed", zap.Error(err))
		}

		healthServer.SetHealthy("order_v1.OrderService")
		grpc_health_v1.RegisterHealthServer(grpcServer.GRPCServer(), healthServer)
		l.Info("health check enabled")

		// Периодическая проверка состояния
		healthCheckCtx, healthCheckCancel := context.WithCancel(context.Background())
		defer healthCheckCancel()
		go func(ctx context.Context) {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					checkCtx, cancel := context.WithTimeout(ctx, healthTimeout)
					if err := health.CheckPostgresHealth(checkCtx, orderStorage.DB()); err == nil {
						healthServer.SetHealthy("postgres")
					} else {
						healthServer.SetUnhealthy("postgres")
						l.Warn("postgres health check failed", zap.Error(err))
					}
					cancel()

					checkCtx, cancel = context.WithTimeout(ctx, healthTimeout)
					if err := health.CheckRedisHealth(checkCtx, redisClient); err == nil {
						healthServer.SetHealthy("redis")
					} else {
						healthServer.SetUnhealthy("redis")
						l.Warn("redis health check failed", zap.Error(err))
					}
					cancel()
				}
			}
		}(healthCheckCtx)
	}

	reflection.Register(grpcServer.GRPCServer())

	// HTTP сервер для /metrics (Prometheus)
	metricsCtx, metricsCancel := context.WithCancel(context.Background())
	defer metricsCancel()
	grpcServer.StartMetricsServer(metricsCtx, cfg.Server.MetricsPort, metricsHandler)

	l.Info("server ready to accept connections")
	if err := grpcServer.Start(); err != nil {
		l.Fatal("server error", zap.Error(err))
	}
}

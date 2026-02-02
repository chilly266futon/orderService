package main

import (
	"flag"
	"log"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	orderv1 "github.com/chilly266futon/orderService/gen/pb"
	"github.com/chilly266futon/orderService/internal/clients"
	"github.com/chilly266futon/orderService/internal/config"
	"github.com/chilly266futon/orderService/internal/service"
	"github.com/chilly266futon/orderService/internal/storage"
	"github.com/chilly266futon/spotService/pkg/shared/breaker"
	"github.com/chilly266futon/spotService/pkg/shared/grpcutil"
	"github.com/chilly266futon/spotService/pkg/shared/health"
	"github.com/chilly266futon/spotService/pkg/shared/interceptors"
	"github.com/chilly266futon/spotService/pkg/shared/logger"
)

const serviceName = "order-service"

func main() {
	// Парсинг флагов
	configPath := flag.String("config", "configs/config.yaml", "Path to config file")
	flag.Parse()

	cfg := config.MustLoad(*configPath)

	l, err := logger.New(cfg.Logger)
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	defer l.Sync()

	l.Info("starting order-service",
		zap.String("version", "1.0.0"),
		zap.String("config", *configPath),
	)

	spotClient, err := clients.NewSpotClient(clients.Config{
		Address:       cfg.SpotService.Addr,
		Timeout:       cfg.SpotService.Timeout,
		EnableBreaker: cfg.SpotService.EnableBreaker,
		BreakerConfig: breaker.Config{
			MaxRequests: cfg.SpotService.Breaker.MaxRequests,
			Interval:    cfg.SpotService.Breaker.Interval,
			Timeout:     cfg.SpotService.Breaker.Timeout,
			Attempts:    cfg.SpotService.Breaker.Attempts,
		},
	}, l)
	if err != nil {
		log.Fatalf("failed to create spot client: %v", err)
	}
	defer spotClient.Close()

	l.Info("connected to spot service",
		zap.String("address", cfg.SpotService.Addr),
		zap.Bool("circuit_creaker", cfg.SpotService.EnableBreaker),
	)

	orderStorage := storage.NewOrderStorage()

	orderService := service.NewService(orderStorage, spotClient, l)

	var interceptorChain []grpc.ServerOption

	interceptorChain = append(interceptorChain,
		grpc.ChainUnaryInterceptor(interceptors.TraceIDInterceptor()),
	)

	interceptorChain = append(interceptorChain,
		grpc.UnaryInterceptor(interceptors.UnaryPanicRecoveryInterceptor(l)),
	)

	if cfg.RateLimit.Enabled {
		rateLimiter := interceptors.NewMethodRateLimiterInterceptor(
			rate.Limit(cfg.RateLimit.RequestsPerSecond),
			cfg.RateLimit.Burst,
		)

		for method, limit := range cfg.RateLimit.Methods {
			rateLimiter.SetMethodLimit(method, rate.Limit(limit.RequestsPerSecond), limit.Burst)
		}

		interceptorChain = append(interceptorChain,
			grpc.ChainUnaryInterceptor(rateLimiter.Interceptor()))

		l.Info("rate limiting enabled")
	}

	interceptorChain = append(interceptorChain,
		grpc.ChainUnaryInterceptor(interceptors.LoggerInterceptor(l)),
	)

	grpcServer, err := grpcutil.NewServer(
		grpcutil.ServerConfig{
			Host:            cfg.Server.Host,
			Port:            cfg.Server.Port,
			ShutdownTimeout: cfg.Server.ShutdownTimeout,
		}, l, interceptorChain...,
	)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	orderv1.RegisterOrderServiceServer(grpcServer.GRPCServer(), orderService)

	// health check
	if cfg.Health.Enabled {
		healthServer := health.NewServer()
		healthServer.SetHealthy("order_v1.OrderService")
		grpc_health_v1.RegisterHealthServer(grpcServer.GRPCServer(), healthServer)
		l.Info("health check enabled")
	}

	reflection.Register(grpcServer.GRPCServer())

	l.Info("server ready to accept connections")
	if err := grpcServer.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

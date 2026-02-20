package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orderpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"
	spotpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/spot"
	"github.com/chilly266futon/orderService/internal/clients"
	"github.com/chilly266futon/orderService/internal/domain"
	"github.com/chilly266futon/orderService/internal/storage"
	"github.com/chilly266futon/spotService/pkg/shared/interceptors"
)

type Service struct {
	orderpb.UnimplementedOrderServiceServer
	storage    *storage.OrderStorage
	spotClient clients.SpotClient
	logger     *zap.Logger
}

func NewService(storage *storage.OrderStorage, spotClient clients.SpotClient, logger *zap.Logger) *Service {
	return &Service{
		storage:    storage,
		spotClient: spotClient,
		logger:     logger,
	}
}

func (s *Service) CreateOrder(ctx context.Context, req *orderpb.CreateOrderRequest) (*orderpb.CreateOrderResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	price, quantity, err := s.validateAndParseCreateOrderRequest(req)
	if err != nil {
		return nil, err
	}

	userRoles := []spotpb.UserRole{spotpb.UserRole_USER_ROLE_COMMON}

	exists, err := s.spotClient.MarketExists(ctx, req.MarketId, userRoles)
	if err != nil {
		s.logger.Error("failed to check market availability",
			zap.String("trace_id", traceID),
			zap.String("market_id", req.MarketId),
			zap.String("user_id", req.UserId),
			zap.Error(err),
		)

		return nil, status.Errorf(codes.Internal, "failed to check market: %v", err)
	}
	if !exists {
		s.logger.Warn("market not found or not accessible",
			zap.String("trace_id", traceID),
			zap.String("market_id", req.MarketId),
			zap.String("user_id", req.UserId),
		)
		return nil, status.Error(codes.NotFound, "market not found or not accessible")
	}

	order := &domain.Order{
		ID:        uuid.NewString(),
		UserID:    req.UserId,
		MarketID:  req.MarketId,
		Type:      req.OrderType,
		Status:    orderpb.OrderStatus_ORDER_STATUS_CREATED,
		Price:     price,
		Quantity:  quantity,
		CreatedAt: time.Now(),
	}

	s.storage.Add(order)

	s.logger.Info("order created",
		zap.String("trace_id", traceID),
		zap.String("order_id", order.ID),
		zap.String("user_id", order.UserID),
		zap.String("market_id", order.MarketID),
	)

	return &orderpb.CreateOrderResponse{
		OrderId: order.ID,
		Status:  order.Status,
	}, nil
}

func (s *Service) GetOrderStatus(ctx context.Context, req *orderpb.GetOrderStatusRequest) (*orderpb.GetOrderStatusResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	order, exists := s.storage.GetByID(req.OrderId)
	if !exists {
		s.logger.Warn("order not found",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderId),
			zap.String("user_id", req.UserId),
		)
		return nil, status.Error(codes.NotFound, "order not found")
	}

	if !order.IsOwnedBy(req.UserId) {
		s.logger.Warn("access denied to order",
			zap.String("trace_id", traceID),
			zap.String("order_id", order.ID),
			zap.String("user_id", req.UserId),
		)
		return nil, status.Error(codes.PermissionDenied, "access denied")
	}

	return &orderpb.GetOrderStatusResponse{
		OrderId: order.ID,
		Status:  order.Status,
	}, nil
}

func (s *Service) validateAndParseCreateOrderRequest(req *orderpb.CreateOrderRequest) (decimal.Decimal, decimal.Decimal, error) {
	if req == nil {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.UserId == "" {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.MarketId == "" {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "market_id is required")
	}
	if req.OrderType == orderpb.OrderType_ORDER_TYPE_UNSPECIFIED {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "order_type is required")
	}
	if req.Price == "" {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "price is required")
	}
	if req.Quantity == "" {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "quantity is required")
	}

	price, err := decimal.NewFromString(req.Price)
	if err != nil {
		return decimal.Zero, decimal.Zero, status.Errorf(codes.InvalidArgument, "invalid price format: %v", err)
	}
	if price.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "price must be greater than zero")
	}

	quantity, err := decimal.NewFromString(req.Quantity)
	if err != nil {
		return decimal.Zero, decimal.Zero, status.Errorf(codes.InvalidArgument, "invalid quantity format: %v", err)
	}
	if quantity.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "quantity must be greater than zero")
	}

	return price, quantity, nil
}

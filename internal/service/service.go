package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orderv1 "github.com/chilly266futon/orderService/gen/pb"
	"github.com/chilly266futon/orderService/internal/clients"
	"github.com/chilly266futon/orderService/internal/domain"
	"github.com/chilly266futon/orderService/internal/storage"
	spotv1 "github.com/chilly266futon/spotService/gen/pb"
	"github.com/chilly266futon/spotService/pkg/shared/interceptors"
)

type Service struct {
	orderv1.UnimplementedOrderServiceServer
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

func (s *Service) CreateOrder(ctx context.Context, req *orderv1.CreateOrderRequest) (*orderv1.CreateOrderResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	price, quantity, err := s.validateAndParseCreateOrderRequest(req)
	if err != nil {
		return nil, err
	}

	userRoles := []spotv1.UserRole{spotv1.UserRole_USER_ROLE_COMMON}

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
		Status:    orderv1.OrderStatus_ORDER_STATUS_CREATED,
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

	return &orderv1.CreateOrderResponse{
		OrderId: order.ID,
		Status:  order.Status,
	}, nil
}

func (s *Service) GetOrderStatus(ctx context.Context, req *orderv1.GetOrderStatusRequest) (*orderv1.GetOrderStatusResponse, error) {
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

	return &orderv1.GetOrderStatusResponse{
		OrderId: order.ID,
		Status:  order.Status,
	}, nil
}

func (s *Service) validateAndParseCreateOrderRequest(req *orderv1.CreateOrderRequest) (decimal.Decimal, decimal.Decimal, error) {
	if req == nil {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.UserId == "" {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.MarketId == "" {
		return decimal.Zero, decimal.Zero, status.Error(codes.InvalidArgument, "market_id is required")
	}
	if req.OrderType == orderv1.OrderType_ORDER_TYPE_UNSPECIFIED {
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

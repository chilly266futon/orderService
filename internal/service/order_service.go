package service

import (
	"context"
	"time"

	spotpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/spot"
	"github.com/chilly266futon/exchange-shared/pkg/common"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/chilly266futon/exchange-shared/pkg/interceptors"

	"github.com/chilly266futon/orderService/internal/clients"
	"github.com/chilly266futon/orderService/internal/domain"
	"github.com/chilly266futon/orderService/internal/dto/order"
	"github.com/chilly266futon/orderService/internal/storage"
)

type OrderUseCase struct {
	storage    *storage.OrderStorage
	spotClient clients.SpotClient
	logger     *zap.Logger
}

func NewOrderUseCase(
	storage *storage.OrderStorage,
	spotClient clients.SpotClient,
	logger *zap.Logger,
) *OrderUseCase {
	return &OrderUseCase{
		storage:    storage,
		spotClient: spotClient,
		logger:     logger,
	}
}

func (uc *OrderUseCase) CreateOrder(ctx context.Context, req order.CreateOrderRequest) (order.CreateOrderResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	if req.Price.IsNegative() || req.Price.IsZero() {
		return order.CreateOrderResponse{}, domain.ErrInvalidPrice
	}
	if req.Quantity.IsNegative() || req.Quantity.IsZero() {
		return order.CreateOrderResponse{}, domain.ErrInvalidQuantity
	}

	ot, err := domain.ParseOrderType(req.OrderType)
	if err != nil {
		return order.CreateOrderResponse{}, err
	}

	userIDFromCtx := common.GetUserID(ctx)
	if userIDFromCtx != "" && userIDFromCtx != req.UserID {
		uc.logger.Warn("user ID from context does not match request",
			zap.String("trace_id", traceID),
			zap.String("user_id_from_ctx", userIDFromCtx),
			zap.String("user_id_from_req", req.UserID),
		)
		return order.CreateOrderResponse{}, domain.ErrAccessDenied
	}

	userRoles := uc.getUserRoles(ctx, req.UserID) // TODO: или передать userIDFromCtx и убрать return выше

	exists, err := uc.spotClient.MarketExists(ctx, req.MarketID, userRoles)
	if err != nil {
		uc.logger.Error("failed to check market availability",
			zap.String("trace_id", traceID),
			zap.String("market_id", req.MarketID),
			zap.String("user_id", req.UserID),
			zap.Error(err),
		)
		return order.CreateOrderResponse{}, status.Errorf(codes.Internal, "failed to check market")
	}
	if !exists {
		uc.logger.Warn("market not found or not accessible",
			zap.String("trace_id", traceID),
			zap.String("market_id", req.MarketID),
			zap.String("user_id", req.UserID),
		)
		return order.CreateOrderResponse{}, domain.ErrMarketNotAvailable
	}

	domainOrder := &domain.Order{
		ID:        uuid.NewString(),
		UserID:    req.UserID,
		MarketID:  req.MarketID,
		Type:      ot,
		Status:    domain.OrderStatusCreated,
		Price:     req.Price,
		Quantity:  req.Quantity,
		CreatedAt: time.Now(),
	}

	uc.storage.Add(domainOrder)

	uc.logger.Info("order created",
		zap.String("trace_id", traceID),
		zap.String("order_id", domainOrder.ID),
		zap.String("user_id", domainOrder.UserID),
		zap.String("market_id", domainOrder.MarketID),
	)

	return order.CreateOrderResponse{
		OrderID: domainOrder.ID,
		Status:  domainOrder.Status.String(),
	}, nil
}

func (uc *OrderUseCase) GetOrderStatus(ctx context.Context, req order.GetOrderStatusRequest) (order.GetOrderStatusResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	orderInfo, exists := uc.storage.GetByID(req.OrderID)
	if !exists {
		uc.logger.Warn("order not found",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", req.UserID),
		)
		return order.GetOrderStatusResponse{}, domain.ErrOrderNotFound
	}
	if req.UserID != orderInfo.UserID {
		uc.logger.Warn("access denied to order",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", req.UserID),
		)
		return order.GetOrderStatusResponse{}, domain.ErrAccessDenied
	}

	return order.GetOrderStatusResponse{
		OrderID: orderInfo.ID,
		Status:  orderInfo.Status.String(),
	}, nil
}

func (uc *OrderUseCase) CancelOrder(ctx context.Context, req order.CancelOrderRequest) (order.CancelOrderResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	orderInfo, exists := uc.storage.GetByID(req.OrderID)
	if !exists {
		uc.logger.Warn("order not found for cancel",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", req.UserID),
		)
		return order.CancelOrderResponse{}, domain.ErrOrderNotFound
	}

	userIDFromCtx := common.GetUserID(ctx)
	if userIDFromCtx != "" && userIDFromCtx != req.UserID {
		uc.logger.Warn("user ID from context does not match request",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id_from_ctx", userIDFromCtx),
			zap.String("user_id_from_req", req.UserID),
		)
		return order.CancelOrderResponse{}, domain.ErrAccessDenied
	}

	if req.UserID != orderInfo.UserID {
		uc.logger.Warn("access denied for cancel",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", req.UserID),
		)
		return order.CancelOrderResponse{}, domain.ErrAccessDenied
	}

	if err := orderInfo.CanBeCancelled(); err != nil {
		uc.logger.Warn("cannot cancel order",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("current_status", orderInfo.Status.String()),
			zap.Error(err),
		)
		return order.CancelOrderResponse{}, err
	}

	// Меняем статус
	orderInfo.Status = domain.OrderStatusCancelled

	uc.storage.Update(orderInfo)

	uc.logger.Info("order cancelled",
		zap.String("trace_id", traceID),
		zap.String("order_id", orderInfo.ID),
		zap.String("user_id", orderInfo.UserID),
	)

	return order.CancelOrderResponse{
		OrderID: orderInfo.ID,
		Status:  orderInfo.Status.String(),
	}, nil

}

func (uc *OrderUseCase) getUserRoles(ctx context.Context, userID string) []spotpb.UserRole {
	// TODO: Implement actual user role retrieval logic, e.g. from auth service or context
	// Пока возвращаем дефолт
	return []spotpb.UserRole{spotpb.UserRole_USER_ROLE_COMMON}

}

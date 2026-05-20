package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	spotpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/spot"

	"github.com/chilly266futon/exchange-shared/pkg/auth"
	"github.com/chilly266futon/exchange-shared/pkg/common"
	"github.com/chilly266futon/exchange-shared/pkg/interceptors"
	"github.com/chilly266futon/orderService/internal/cache"
	"github.com/chilly266futon/orderService/internal/clients"
	"github.com/chilly266futon/orderService/internal/config"
	"github.com/chilly266futon/orderService/internal/domain"
	"github.com/chilly266futon/orderService/internal/dto/order"
	"github.com/chilly266futon/orderService/internal/storage"
)

const (
	maxPriceValue    = 1_000_000_000 // 1 billion
	maxQuantityValue = 1_000_000     // 1 million
)

// OrderRepository defines the storage operations for orders.
type OrderRepository interface {
	Create(ctx context.Context, order *domain.Order) error
	GetByID(ctx context.Context, orderID string) (*domain.Order, error)
	Update(ctx context.Context, order *domain.Order) error
	CountActiveByUserID(ctx context.Context, userID string) (int, error)
	CancelOrderAtomic(ctx context.Context, orderID, userID string, expectedStatus domain.OrderStatus) error
}

// OrderService defines the business logic operations for orders.
type OrderService interface {
	CreateOrder(ctx context.Context, req order.CreateOrderRequest) (order.CreateOrderResponse, error)
	GetOrderStatus(ctx context.Context, req order.GetOrderStatusRequest) (order.GetOrderStatusResponse, error)
	CancelOrder(ctx context.Context, req order.CancelOrderRequest) (order.CancelOrderResponse, error)
}

// MarketAccessCache defines operations for caching market accessibility checks.
type MarketAccessCache interface {
	IsMarketAccessible(ctx context.Context, marketID string, roles []string) (bool, error)
	CacheMarketAccessibility(ctx context.Context, marketID string, roles []string, accessible bool) error
}

// Metrics defines the telemetry operations for order-related events.
type Metrics interface {
	IncOrderCreated(ctx context.Context, marketID string, orderType string)
	IncOrderCancelled(ctx context.Context)
	RecordAverageOrderValue(ctx context.Context, value float64)
	IncTotalRevenue(ctx context.Context, amount float64)
	IncOrderStatus(ctx context.Context, status string)
}

// OrderUseCase implements the OrderService interface and contains the core business logic for orders.
type OrderUseCase struct {
	orderRepo         storage.OrderRepository
	spotClient        clients.SpotClient
	marketAccess      MarketAccessCache
	orderLimits       config.OrderLimits
	metrics           Metrics
	logger            *zap.Logger
	activeOrdersCache cache.Cache // optional cache for active order counts
}

// NewOrderUseCase creates a new instance of OrderUseCase with the provided dependencies.
func NewOrderUseCase(
	orderRepo storage.OrderRepository,
	spotClient clients.SpotClient,
	marketAccess MarketAccessCache,
	orderLimits config.OrderLimits,
	metrics Metrics,
	logger *zap.Logger,
	activeOrdersCache cache.Cache,
) *OrderUseCase {
	return &OrderUseCase{
		orderRepo:         orderRepo,
		spotClient:        spotClient,
		marketAccess:      marketAccess,
		orderLimits:       orderLimits,
		metrics:           metrics,
		logger:            logger,
		activeOrdersCache: activeOrdersCache,
	}
}

// CreateOrder validates the request, checks market accessibility, enforces order limits, and creates a new order.
func (uc *OrderUseCase) CreateOrder(ctx context.Context, req order.CreateOrderRequest) (order.CreateOrderResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	authCtx := interceptors.ToAuthCtx(ctx)
	if authCtx.UserID == "" {
		return order.CreateOrderResponse{}, status.Error(codes.Unauthenticated, "authentication required")
	}

	if !auth.HasPermissionFromSet(authCtx.Permissions, "trade:spot") {
		return order.CreateOrderResponse{}, status.Error(codes.PermissionDenied, "insufficient permissions")
	}

	if req.Price.IsNegative() || req.Price.IsZero() {
		return order.CreateOrderResponse{}, domain.ErrInvalidPrice
	}
	if req.Quantity.IsNegative() || req.Quantity.IsZero() {
		return order.CreateOrderResponse{}, domain.ErrInvalidQuantity
	}
	// Validate maximum values
	maxPriceDec := decimal.NewFromInt(maxPriceValue)
	maxQuantityDec := decimal.NewFromInt(maxQuantityValue)
	if req.Price.Cmp(maxPriceDec) > 0 {
		uc.logger.Warn("price exceeds maximum allowed",
			zap.String("trace_id", traceID),
			zap.String("user_id", req.UserID),
			zap.String("price", req.Price.String()),
		)
		return order.CreateOrderResponse{}, domain.ErrInvalidPrice
	}
	if req.Quantity.Cmp(maxQuantityDec) > 0 {
		uc.logger.Warn("quantity exceeds maximum allowed",
			zap.String("trace_id", traceID),
			zap.String("user_id", req.UserID),
			zap.String("quantity", req.Quantity.String()),
		)
		return order.CreateOrderResponse{}, domain.ErrInvalidQuantity
	}

	userRoles := uc.getUserRoles(ctx)

	accessible, err := uc.marketAccess.IsMarketAccessible(ctx, req.MarketID, userRoles)
	if errors.Is(err, cache.ErrNotFound) {
		// cache empty - go to spot service
		uc.logger.Debug("market access cache miss",
			zap.String("trace_id", traceID),
			zap.String("market_id", req.MarketID),
			zap.String("user_id", req.UserID),
		)
		pbRoles := uc.rolesToPb(userRoles)

		exists, err := uc.spotClient.MarketExists(ctx, req.MarketID, pbRoles)
		if err != nil {
			uc.logger.Error("failed to check market availability",
				zap.String("trace_id", traceID),
				zap.String("market_id", req.MarketID),
				zap.String("user_id", req.UserID),
				zap.Error(err),
			)
			return order.CreateOrderResponse{}, toGRPCError(err)
		}

		// cache the result
		if cacheErr := uc.marketAccess.CacheMarketAccessibility(ctx, req.MarketID, userRoles, exists); cacheErr != nil {
			uc.logger.Warn("failed to cache market accessibility",
				zap.String("trace_id", traceID),
				zap.String("market_id", req.MarketID),
				zap.Error(cacheErr),
			)
			// non-fatal error - continue without cache
		}

		if !exists {
			uc.logger.Warn("market not found or not accessible",
				zap.String("trace_id", traceID),
				zap.String("market_id", req.MarketID),
				zap.String("user_id", req.UserID),
			)
			return order.CreateOrderResponse{}, domain.ErrMarketNotAvailable
		}
	} else if err != nil {
		uc.logger.Error("failed to check market access cache",
			zap.String("trace_id", traceID),
			zap.String("market_id", req.MarketID),
			zap.String("user_id", req.UserID),
			zap.Error(err),
		)
		return order.CreateOrderResponse{}, toGRPCError(err)
	} else if !accessible {
		uc.logger.Warn("market not accessible (cached)",
			zap.String("trace_id", traceID),
			zap.String("market_id", req.MarketID),
			zap.String("user_id", req.UserID),
		)
		return order.CreateOrderResponse{}, domain.ErrMarketNotAvailable
	}

	// Check active order limit per role
	maxActive := uc.maxActiveOrdersForRoles(userRoles)
	activeCount, err := uc.getActiveOrderCount(ctx, req.UserID)
	if err != nil {
		uc.logger.Error("failed to count active orders",
			zap.String("trace_id", traceID),
			zap.String("user_id", req.UserID),
			zap.Error(err),
		)
		return order.CreateOrderResponse{}, toGRPCError(err)
	}
	if activeCount >= maxActive {
		uc.logger.Warn("active order limit exceeded",
			zap.String("trace_id", traceID),
			zap.String("user_id", req.UserID),
			zap.Int("active", activeCount),
			zap.Int("limit", maxActive),
			zap.Strings("roles", userRoles),
		)
		return order.CreateOrderResponse{}, domain.ErrActiveOrderLimitExceeded
	}

	domainOrder := &domain.Order{
		ID:        uuid.NewString(),
		UserID:    req.UserID,
		MarketID:  req.MarketID,
		Type:      req.OrderType,
		Status:    domain.OrderStatusCreated,
		Price:     req.Price,
		Quantity:  req.Quantity,
		CreatedAt: time.Now(),
	}

	if err = uc.orderRepo.Create(ctx, domainOrder); err != nil {
		uc.logger.Error("failed to create order",
			zap.String("trace_id", traceID),
			zap.String("order_id", domainOrder.ID),
			zap.Error(err),
		)
		return order.CreateOrderResponse{}, toGRPCError(err)
	}

	uc.logger.Info("order created",
		zap.String("trace_id", traceID),
		zap.String("order_id", domainOrder.ID),
		zap.String("user_id", domainOrder.UserID),
		zap.String("market_id", domainOrder.MarketID),
	)

	uc.metrics.IncOrderCreated(ctx, domainOrder.MarketID, domainOrder.Type.String())
	uc.metrics.IncOrderStatus(ctx, domain.OrderStatusCreated.String())

	// Metrics: average order value and total revenue
	if uc.metrics != nil {
		price, _ := req.Price.Float64()
		quantity, _ := req.Quantity.Float64()

		orderValue := price * quantity
		uc.metrics.RecordAverageOrderValue(ctx, orderValue)
		uc.metrics.IncTotalRevenue(ctx, orderValue)
	}

	// Invalidate active order count cache for user
	if uc.activeOrdersCache != nil {
		key := "active_orders:" + req.UserID
		_ = uc.activeOrdersCache.Delete(ctx, key)
	}

	return order.CreateOrderResponse{
		OrderID: domainOrder.ID,
		Status:  domainOrder.Status.String(),
	}, nil
}

// GetOrderStatus retrieves the status of an order, ensuring the requesting user owns the order.
func (uc *OrderUseCase) GetOrderStatus(ctx context.Context, req order.GetOrderStatusRequest) (order.GetOrderStatusResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	userID := common.GetUserID(ctx)
	if userID == "" {
		return order.GetOrderStatusResponse{}, status.Error(codes.Unauthenticated, "authentication required")
	}

	orderInfo, err := uc.orderRepo.GetByID(ctx, req.OrderID)
	if err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			uc.logger.Warn("order not found",
				zap.String("trace_id", traceID),
				zap.String("order_id", req.OrderID),
				zap.String("user_id", userID),
			)
			return order.GetOrderStatusResponse{}, err
		}
		uc.logger.Error("failed to get order",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return order.GetOrderStatusResponse{}, toGRPCError(err)
	}
	if userID != orderInfo.UserID {
		uc.logger.Warn("access denied to order",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", userID),
		)
		return order.GetOrderStatusResponse{}, domain.ErrAccessDenied
	}

	return order.GetOrderStatusResponse{
		OrderID: orderInfo.ID,
		Status:  orderInfo.Status.String(),
	}, nil
}

// CancelOrder cancels an order if it is cancellable and belongs to the requesting user.
func (uc *OrderUseCase) CancelOrder(ctx context.Context, req order.CancelOrderRequest) (order.CancelOrderResponse, error) {
	traceID := interceptors.GetTraceID(ctx)

	userID := common.GetUserID(ctx)
	if userID == "" {
		return order.CancelOrderResponse{}, status.Error(codes.Unauthenticated, "authentication required")
	}

	orderInfo, err := uc.orderRepo.GetByID(ctx, req.OrderID)
	if err != nil {
		if errors.Is(err, domain.ErrOrderNotFound) {
			uc.logger.Warn("order not found for cancel",
				zap.String("trace_id", traceID),
				zap.String("order_id", req.OrderID),
				zap.String("user_id", userID),
			)
			return order.CancelOrderResponse{}, err
		}
		uc.logger.Error("failed to get order for cancel",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return order.CancelOrderResponse{}, toGRPCError(err)
	}

	if userID != orderInfo.UserID {
		uc.logger.Warn("access denied for cancel",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", userID),
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

	// Atomically cancel order
	if err := uc.orderRepo.CancelOrderAtomic(ctx, req.OrderID, userID, orderInfo.Status); err != nil {
		uc.logger.Error("failed to cancel order atomically",
			zap.String("trace_id", traceID),
			zap.String("order_id", req.OrderID),
			zap.String("user_id", userID),
			zap.Error(err),
		)
		return order.CancelOrderResponse{}, toGRPCError(err)
	}

	// Update local object status for response
	orderInfo.Status = domain.OrderStatusCancelled

	uc.logger.Info("order cancelled",
		zap.String("trace_id", traceID),
		zap.String("order_id", orderInfo.ID),
		zap.String("market_id", orderInfo.MarketID),
		zap.String("order_type", orderInfo.Type.String()),
		zap.String("price", orderInfo.Price.String()),
		zap.String("quantity", orderInfo.Quantity.String()),
		zap.String("user_id", orderInfo.UserID),
	)

	uc.metrics.IncOrderCancelled(ctx)
	uc.metrics.IncOrderStatus(ctx, domain.OrderStatusCancelled.String())

	// Invalidate active order count cache for user
	if uc.activeOrdersCache != nil {
		key := "active_orders:" + userID
		_ = uc.activeOrdersCache.Delete(ctx, key)
	}

	return order.CancelOrderResponse{
		OrderID: orderInfo.ID,
		Status:  orderInfo.Status.String(),
	}, nil

}

// maxActiveOrdersForRoles returns the maximum active order limit among all user roles (the highest is taken).
func (uc *OrderUseCase) maxActiveOrdersForRoles(roles []string) int {
	maxLimit := uc.orderLimits.Default
	for _, role := range roles {
		if limit, ok := uc.orderLimits.MaxActiveOrders[role]; ok && limit > maxLimit {
			maxLimit = limit
		}
	}
	return maxLimit
}

// getActiveOrderCount returns the number of active orders for a user, using cache if available.
func (uc *OrderUseCase) getActiveOrderCount(ctx context.Context, userID string) (int, error) {
	const cacheTTL = 30 * time.Second
	if uc.activeOrdersCache != nil {
		key := "active_orders:" + userID
		val, err := uc.activeOrdersCache.Get(ctx, key)
		if err == nil {
			// cache hit
			count, err := strconv.Atoi(val)
			if err == nil {
				return count, nil
			}
			// if conversion fails, fall through to DB
		}
		// if error (including ErrNotFound), proceed to DB
	}
	count, err := uc.orderRepo.CountActiveByUserID(ctx, userID)
	if err != nil {
		return 0, err
	}
	if uc.activeOrdersCache != nil {
		key := "active_orders:" + userID
		// ignore error, caching is best-effort
		_ = uc.activeOrdersCache.Set(ctx, key, strconv.Itoa(count), cacheTTL)
	}
	return count, nil
}

func (uc *OrderUseCase) getUserRoles(ctx context.Context) []string {
	// Try to get as []int32 (as stored in context by auth interceptor)
	if rolesInt, ok := ctx.Value("roles").([]int32); ok && len(rolesInt) > 0 {
		var roles []string
		for _, r := range rolesInt {
			switch r {
			case 1:
				roles = append(roles, "COMMON")
			case 2:
				roles = append(roles, "VERIFIED")
			case 3:
				roles = append(roles, "PREMIUM")
			case 4:
				roles = append(roles, "ADMIN")
			default:
				uc.logger.Warn("unknown role numeric value", zap.Int32("role", r))
				roles = append(roles, "COMMON")
			}
		}
		return roles
	}
	// Try to get as []string (in case strings are already present)
	if rolesStr, ok := ctx.Value("roles").([]string); ok && len(rolesStr) > 0 {
		return rolesStr
	}
	// Roles not found
	uc.logger.Warn("no roles found in context, using default")
	return []string{"COMMON"}
}

func (uc *OrderUseCase) rolesToPb(roles []string) []spotpb.UserRole {
	var protoRoles []spotpb.UserRole
	roleMap := map[string]spotpb.UserRole{
		"COMMON":   spotpb.UserRole_USER_ROLE_COMMON,
		"VERIFIED": spotpb.UserRole_USER_ROLE_VERIFIED,
		"PREMIUM":  spotpb.UserRole_USER_ROLE_PREMIUM,
		"ADMIN":    spotpb.UserRole_USER_ROLE_ADMIN,
	}
	for _, role := range roles {
		if pbRole, ok := roleMap[role]; ok {
			protoRoles = append(protoRoles, pbRole)
		} else {
			uc.logger.Warn("unknown role encountered", zap.String("role", role))
			protoRoles = append(protoRoles, spotpb.UserRole_USER_ROLE_UNSPECIFIED)
		}
	}

	if len(protoRoles) == 0 {
		uc.logger.Warn("no valid roles found, defaulting to COMMON")
		return []spotpb.UserRole{spotpb.UserRole_USER_ROLE_COMMON}
	}

	return protoRoles
}

// toGRPCError converts an error to the appropriate gRPC status.
func toGRPCError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "request canceled")
	}

	// Handle domain errors
	switch {
	case errors.Is(err, domain.ErrInvalidPrice),
		errors.Is(err, domain.ErrInvalidQuantity),
		errors.Is(err, domain.ErrInvalidOrderType),
		errors.Is(err, domain.ErrInvalidOrderStatus):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, domain.ErrOrderNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, domain.ErrAccessDenied):
		return status.Error(codes.PermissionDenied, err.Error())

	case errors.Is(err, domain.ErrMarketNotAvailable):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, domain.ErrActiveOrderLimitExceeded):
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, domain.ErrOrderCannotBeCancelled),
		errors.Is(err, domain.ErrOrderAlreadyCancelled):
		return status.Error(codes.FailedPrecondition, err.Error())
	}

	// If it's already a gRPC status, return as is
	if s, ok := status.FromError(err); ok && s.Code() != codes.Unknown {
		return err
	}

	// Default to internal error
	return status.Error(codes.Internal, "internal error")
}

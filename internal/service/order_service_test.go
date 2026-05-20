package service

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/chilly266futon/orderService/internal/config"
	"github.com/chilly266futon/orderService/internal/domain"
	"github.com/chilly266futon/orderService/internal/dto/order"

	"github.com/chilly266futon/exchange-shared/pkg/common"
	"github.com/chilly266futon/exchange-shared/pkg/interceptors"
)

type mockOrderRepo struct{ mock.Mock }

func (m *mockOrderRepo) CountActiveByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}
func (m *mockOrderRepo) Create(ctx context.Context, order *domain.Order) error { return nil }
func (m *mockOrderRepo) GetByID(ctx context.Context, orderID string) (*domain.Order, error) {
	args := m.Called(ctx, orderID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Order), args.Error(1)
}
func (m *mockOrderRepo) Update(ctx context.Context, order *domain.Order) error { return nil }
func (m *mockOrderRepo) CancelOrderAtomic(ctx context.Context, orderID, userID string, expectedStatus domain.OrderStatus) error {
	args := m.Called(ctx, orderID, userID, expectedStatus)
	return args.Error(0)
}

type mockSpotClient struct{ mock.Mock }

func (m *mockSpotClient) MarketExists(ctx context.Context, marketID string, roles []interface{}) (bool, error) {
	args := m.Called(ctx, marketID, roles)
	return args.Bool(0), args.Error(1)
}

type mockMarketAccess struct{ mock.Mock }

func (m *mockMarketAccess) IsMarketAccessible(ctx context.Context, marketID string, roles []string) (bool, error) {
	args := m.Called(ctx, marketID, roles)
	return args.Bool(0), args.Error(1)
}
func (m *mockMarketAccess) CacheMarketAccessibility(ctx context.Context, marketID string, roles []string, accessible bool) error {
	args := m.Called(ctx, marketID, roles, accessible)
	return args.Error(0)
}

type mockMetrics struct{ mock.Mock }

func (m *mockMetrics) IncOrderCreated(ctx context.Context, marketID string, orderType string) {
	m.Called(ctx, marketID, orderType)
}
func (m *mockMetrics) IncOrderCancelled(ctx context.Context) {
	m.Called(ctx)
}
func (m *mockMetrics) RecordAverageOrderValue(ctx context.Context, value float64) {
	m.Called(ctx, value)
}
func (m *mockMetrics) IncTotalRevenue(ctx context.Context, amount float64) {
	m.Called(ctx, amount)
}
func (m *mockMetrics) IncOrderStatus(ctx context.Context, status string) {
	m.Called(ctx, status)
}

func setUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, common.UserIDKey, userID)
}

// createAuthContext создаёт контекст с аутентификационными данными (user_id, roles, permissions).
func createAuthContext(userID string, permissions map[string]struct{}) context.Context {
	authCtx := interceptors.AuthCtx{
		UserID:      userID,
		Roles:       []int32{}, // пустые роли для тестов
		Permissions: permissions,
	}
	return interceptors.NewContextWithAuthCtx(context.Background(), authCtx)
}

func TestCreateOrder_NoPermissions(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	useCase := &OrderUseCase{orderRepo: orderRepo}
	ctx := context.Background()
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCreateOrder_NoTradeSpotPermission(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	useCase := &OrderUseCase{orderRepo: orderRepo}
	perms := map[string]struct{}{"read:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCreateOrder_InvalidPrice(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	useCase := &OrderUseCase{orderRepo: orderRepo}
	perms := map[string]struct{}{"trade:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(0), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCreateOrder_InvalidQuantity(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	useCase := &OrderUseCase{orderRepo: orderRepo}
	perms := map[string]struct{}{"trade:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(0), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCreateOrder_MarketNotAccessible(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	marketAccess := new(mockMarketAccess)
	metrics := new(mockMetrics)
	marketAccess.On("IsMarketAccessible", mock.Anything, "m1", mock.Anything).Return(false, nil)
	useCase := NewOrderUseCase(orderRepo, nil, marketAccess, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	perms := map[string]struct{}{"trade:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCreateOrder_ActiveOrderLimitExceeded(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	orderRepo.On("CountActiveByUserID", mock.Anything, "u1").Return(10, nil)
	marketAccess := new(mockMarketAccess)
	metrics := new(mockMetrics)
	marketAccess.On("IsMarketAccessible", mock.Anything, "m1", mock.Anything).Return(true, nil)
	useCase := NewOrderUseCase(orderRepo, nil, marketAccess, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	perms := map[string]struct{}{"trade:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCreateOrder_Success(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	orderRepo.On("CountActiveByUserID", mock.Anything, "u1").Return(0, nil)
	orderRepo.On("Create", mock.Anything, mock.Anything).Return(nil)
	marketAccess := new(mockMarketAccess)
	metrics := new(mockMetrics)
	metrics.On("IncOrderCreated", mock.Anything, mock.Anything, mock.Anything).Return()
	metrics.On("IncOrderStatus", mock.Anything, domain.OrderStatusCreated.String()).Return()
	metrics.On("RecordAverageOrderValue", mock.Anything, mock.Anything).Return()
	metrics.On("IncTotalRevenue", mock.Anything, mock.Anything).Return()
	marketAccess.On("IsMarketAccessible", mock.Anything, "m1", mock.Anything).Return(true, nil)
	useCase := NewOrderUseCase(orderRepo, nil, marketAccess, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	perms := map[string]struct{}{"trade:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.OrderID)
}

func TestGetOrderStatus_NoUserID(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	ctx := context.Background() // userID отсутствует
	req := order.GetOrderStatusRequest{OrderID: "order1"}
	resp, err := useCase.GetOrderStatus(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestGetOrderStatus_OrderNotFound(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderRepo.On("GetByID", mock.Anything, "order1").Return(nil, domain.ErrOrderNotFound)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.GetOrderStatus(ctx, order.GetOrderStatusRequest{OrderID: "order1"})
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestGetOrderStatus_AccessDenied(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderObj := &domain.Order{ID: "order1", UserID: "u2", Status: domain.OrderStatusCreated}
	orderRepo.On("GetByID", mock.Anything, "order1").Return(orderObj, nil)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.GetOrderStatus(ctx, order.GetOrderStatusRequest{OrderID: "order1"})
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestGetOrderStatus_Success(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderObj := &domain.Order{ID: "order1", UserID: "u1", Status: domain.OrderStatusCreated}
	orderRepo.On("GetByID", mock.Anything, "order1").Return(orderObj, nil)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.GetOrderStatus(ctx, order.GetOrderStatusRequest{OrderID: "order1"})
	assert.NoError(t, err)
	assert.Equal(t, "order1", resp.OrderID)
	assert.Equal(t, domain.OrderStatusCreated.String(), resp.Status)
}

func TestCancelOrder_NoUserID(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	ctx := context.Background()
	req := order.CancelOrderRequest{OrderID: "order1"}
	resp, err := useCase.CancelOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCancelOrder_OrderNotFound(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderRepo.On("GetByID", mock.Anything, "order1").Return(nil, domain.ErrOrderNotFound)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.CancelOrder(ctx, order.CancelOrderRequest{OrderID: "order1"})
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCancelOrder_AccessDenied(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderObj := &domain.Order{ID: "order1", UserID: "u2", Status: domain.OrderStatusCreated}
	orderRepo.On("GetByID", mock.Anything, "order1").Return(orderObj, nil)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.CancelOrder(ctx, order.CancelOrderRequest{OrderID: "order1"})
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCancelOrder_CannotBeCancelled(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderObj := &domain.Order{ID: "order1", UserID: "u1", Status: domain.OrderStatusFilled}
	orderRepo.On("GetByID", mock.Anything, "order1").Return(orderObj, nil)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.CancelOrder(ctx, order.CancelOrderRequest{OrderID: "order1"})
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}
func TestCancelOrder_Success(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderObj := &domain.Order{ID: "order1", UserID: "u1", Status: domain.OrderStatusCreated}
	orderRepo.On("GetByID", mock.Anything, "order1").Return(orderObj, nil)
	orderRepo.On("CancelOrderAtomic", mock.Anything, "order1", "u1", domain.OrderStatusCreated).Return(nil)
	metrics.On("IncOrderCancelled", mock.Anything).Return()
	metrics.On("IncOrderStatus", mock.Anything, domain.OrderStatusCancelled.String()).Return()
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.CancelOrder(ctx, order.CancelOrderRequest{OrderID: "order1"})
	assert.NoError(t, err)
	assert.Equal(t, "order1", resp.OrderID)
	assert.Equal(t, domain.OrderStatusCancelled.String(), resp.Status)
}

func TestCreateOrder_MarketAccessTimeout(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	marketAccess := new(mockMarketAccess)
	metrics := new(mockMetrics)
	marketAccess.On("IsMarketAccessible", mock.Anything, "m1", mock.Anything).Return(false, context.DeadlineExceeded)
	useCase := NewOrderUseCase(orderRepo, nil, marketAccess, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	perms := map[string]struct{}{"trade:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestCreateOrder_CountActiveTimeout(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	marketAccess := new(mockMarketAccess)
	metrics := new(mockMetrics)
	marketAccess.On("IsMarketAccessible", mock.Anything, "m1", mock.Anything).Return(true, nil)
	orderRepo.On("CountActiveByUserID", mock.Anything, "u1").Return(0, context.DeadlineExceeded)
	useCase := NewOrderUseCase(orderRepo, nil, marketAccess, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	perms := map[string]struct{}{"trade:spot": {}}
	ctx := createAuthContext("u1", perms)
	req := order.CreateOrderRequest{UserID: "u1", Price: decimal.NewFromInt(1), Quantity: decimal.NewFromInt(1), MarketID: "m1"}
	resp, err := useCase.CreateOrder(ctx, req)
	assert.Error(t, err)
	assert.Empty(t, resp.OrderID)
}

func TestGetOrderStatus_Timeout(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderRepo.On("GetByID", mock.Anything, "order1").Return(nil, context.DeadlineExceeded)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.GetOrderStatus(ctx, order.GetOrderStatusRequest{OrderID: "order1"})
	assert.Error(t, err)
	assert.Contains(t, []codes.Code{codes.DeadlineExceeded, codes.Canceled}, status.Code(err))
	assert.Empty(t, resp.OrderID)
}

func TestCancelOrder_Timeout(t *testing.T) {
	orderRepo := new(mockOrderRepo)
	metrics := new(mockMetrics)
	orderRepo.On("GetByID", mock.Anything, "order1").Return(nil, context.DeadlineExceeded)
	ctx := setUserID(context.Background(), "u1")
	useCase := NewOrderUseCase(orderRepo, nil, nil, config.OrderLimits{Default: 5, MaxActiveOrders: map[string]int{"COMMON": 5}}, metrics, zap.NewNop(), nil)
	resp, err := useCase.CancelOrder(ctx, order.CancelOrderRequest{OrderID: "order1"})
	assert.Error(t, err)
	assert.Contains(t, []codes.Code{codes.DeadlineExceeded, codes.Canceled}, status.Code(err))
	assert.Empty(t, resp.OrderID)
}

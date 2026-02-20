package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orderpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"
	spotpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/spot"
	"github.com/chilly266futon/orderService/internal/storage"
)

type fakeSpotClient struct {
	existingMarkets map[string]bool
	err             error
}

func (f *fakeSpotClient) MarketExists(ctx context.Context, marketID string, userRoles []spotpb.UserRole) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.existingMarkets[marketID], f.err
}

func (f *fakeSpotClient) Close() error {
	return nil
}

func newTestService(spotClient *fakeSpotClient) *Service {
	logger := zap.NewNop()
	return NewService(storage.NewOrderStorage(), spotClient, logger)
}

func TestCreateOrder_Success(t *testing.T) {
	spotClient := &fakeSpotClient{
		existingMarkets: map[string]bool{"BTC-USDT": true},
	}

	svc := newTestService(spotClient)

	resp, err := svc.CreateOrder(context.Background(), &orderpb.CreateOrderRequest{
		UserId:    "user-1",
		MarketId:  "BTC-USDT",
		OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
		Price:     "50000.00",
		Quantity:  "1.5",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.OrderId)
	assert.Equal(t, orderpb.OrderStatus_ORDER_STATUS_CREATED, resp.Status)
}

func TestCreateOrder_MarketNotFound(t *testing.T) {
	spotClient := &fakeSpotClient{
		existingMarkets: map[string]bool{},
	}

	svc := newTestService(spotClient)

	_, err := svc.CreateOrder(context.Background(), &orderpb.CreateOrderRequest{
		UserId:    "user-1",
		MarketId:  "ETH-USDT",
		OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
		Price:     "3000.00",
		Quantity:  "10",
	})

	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestCreateOrder_SpotServiceError(t *testing.T) {
	spotClient := &fakeSpotClient{
		err: errors.New("spot service unavailable"),
	}

	svc := newTestService(spotClient)

	_, err := svc.CreateOrder(context.Background(), &orderpb.CreateOrderRequest{
		UserId:    "user-1",
		MarketId:  "BTC-USDT",
		OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
		Price:     "50000",
		Quantity:  "1",
	})

	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestCreateOrder_InvalidArguments(t *testing.T) {
	spotClient := &fakeSpotClient{
		existingMarkets: map[string]bool{"BTC": true},
	}

	svc := newTestService(spotClient)

	tests := []struct {
		name string
		req  *orderpb.CreateOrderRequest
	}{
		{
			name: "empty user_id",
			req: &orderpb.CreateOrderRequest{
				MarketId:  "BTC",
				OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
				Price:     "1",
				Quantity:  "1",
			},
		},
		{
			name: "price <= 0",
			req: &orderpb.CreateOrderRequest{
				UserId:    "u",
				MarketId:  "BTC",
				OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
				Price:     "0",
				Quantity:  "1",
			},
		},
		{
			name: "invalid price format",
			req: &orderpb.CreateOrderRequest{
				UserId:    "u",
				MarketId:  "BTC",
				OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
				Price:     "invalid",
				Quantity:  "1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateOrder(context.Background(), tt.req)
			require.Error(t, err)

			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

func TestGetOrderStatus_Success(t *testing.T) {
	spotClient := &fakeSpotClient{
		existingMarkets: map[string]bool{"BTC": true},
	}

	svc := newTestService(spotClient)

	createResp, err := svc.CreateOrder(context.Background(), &orderpb.CreateOrderRequest{
		UserId:    "user-1",
		MarketId:  "BTC",
		OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
		Price:     "10",
		Quantity:  "1",
	})
	require.NoError(t, err)

	resp, err := svc.GetOrderStatus(context.Background(), &orderpb.GetOrderStatusRequest{
		OrderId: createResp.OrderId,
		UserId:  "user-1",
	})

	require.NoError(t, err)
	assert.Equal(t, orderpb.OrderStatus_ORDER_STATUS_CREATED, resp.Status)
}

func TestGetOrderStatus_AccessDenied(t *testing.T) {
	spotClient := &fakeSpotClient{
		existingMarkets: map[string]bool{"BTC": true},
	}

	svc := newTestService(spotClient)

	createResp, err := svc.CreateOrder(context.Background(), &orderpb.CreateOrderRequest{
		UserId:    "user-1",
		MarketId:  "BTC",
		OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
		Price:     "10",
		Quantity:  "1",
	})

	_, err = svc.GetOrderStatus(context.Background(), &orderpb.GetOrderStatusRequest{
		OrderId: createResp.OrderId,
		UserId:  "user-2",
	})

	require.Error(t, err)

	st, _ := status.FromError(err)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

func TestGetOrderStatus_NotFound(t *testing.T) {
	spotClient := &fakeSpotClient{}
	svc := newTestService(spotClient)

	_, err := svc.GetOrderStatus(context.Background(), &orderpb.GetOrderStatusRequest{
		OrderId: "non-existent-order",
		UserId:  "user-1",
	})

	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetOrderStatus_InvalidArguments(t *testing.T) {
	svc := newTestService(&fakeSpotClient{})

	tests := []struct {
		name string
		req  *orderpb.GetOrderStatusRequest
	}{
		{
			name: "nil request",
			req:  nil,
		},
		{
			name: "empty order_id",
			req: &orderpb.GetOrderStatusRequest{
				UserId: "user-1",
			},
		},
		{
			name: "empty user_id",
			req: &orderpb.GetOrderStatusRequest{
				OrderId: "order-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.GetOrderStatus(context.Background(), tt.req)
			require.Error(t, err)

			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

func TestCreateOrder_NilRequest(t *testing.T) {
	svc := newTestService(&fakeSpotClient{})

	_, err := svc.CreateOrder(context.Background(), nil)

	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateOrder_InvalidQuantitty(t *testing.T) {
	spotClient := &fakeSpotClient{
		existingMarkets: map[string]bool{"BTC": true},
	}
	svc := newTestService(spotClient)

	tests := []struct {
		name     string
		quantity string
	}{
		{"quantity <= 0", "0"},
		{"negative quantity", "-1"},
		{"invalid format", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateOrder(context.Background(), &orderpb.CreateOrderRequest{
				UserId:    "user-1",
				MarketId:  "BTC",
				OrderType: orderpb.OrderType_ORDER_TYPE_LIMIT,
				Price:     "100",
				Quantity:  tt.quantity,
			})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

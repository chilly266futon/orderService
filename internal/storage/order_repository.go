package storage

import (
	"context"

	"github.com/chilly266futon/orderService/internal/domain"
)

type OrderRepository interface {
	Create(ctx context.Context, order *domain.Order) error
	GetByID(ctx context.Context, orderID string) (*domain.Order, error)
	Update(ctx context.Context, order *domain.Order) error
	CountActiveByUserID(ctx context.Context, userID string) (int, error)
	CancelOrderAtomic(ctx context.Context, orderID, userID string, expectedStatus domain.OrderStatus) error
}

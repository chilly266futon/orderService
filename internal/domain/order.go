package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

type Order struct {
	ID        string
	UserID    string
	MarketID  string
	Type      OrderType
	Status    OrderStatus
	Price     decimal.Decimal
	Quantity  decimal.Decimal
	CreatedAt time.Time
}

func (o *Order) IsOwnedBy(userID string) bool {
	return o.UserID == userID
}

func (o *Order) CanBeCancelled() error {
	switch o.Status {
	case OrderStatusCreated, OrderStatusOpen:
		return nil
	case OrderStatusFilled, OrderStatusRejected:
		return ErrOrderCannotBeCancelled
	case OrderStatusCancelled:
		return ErrOrderAlreadyCancelled
	default:
		return ErrInvalidOrderStatus
	}
}

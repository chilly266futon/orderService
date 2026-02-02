package domain

import (
	"time"

	"github.com/shopspring/decimal"

	orderv1 "github.com/chilly266futon/orderService/gen/pb"
)

type Order struct {
	ID        string
	UserID    string
	MarketID  string
	Type      orderv1.OrderType
	Status    orderv1.OrderStatus
	Price     decimal.Decimal
	Quantity  decimal.Decimal
	CreatedAt time.Time
}

func (o *Order) IsOwnedBy(userID string) bool {
	return o.UserID == userID
}

func (o *Order) CanBeCancelled() bool {
	return o.Status == orderv1.OrderStatus_ORDER_STATUS_CANCELLED ||
		o.Status == orderv1.OrderStatus_ORDER_STATUS_OPEN
}

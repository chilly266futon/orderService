package domain

import (
	"time"

	"github.com/shopspring/decimal"

	orderpb "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"
)

type Order struct {
	ID        string
	UserID    string
	MarketID  string
	Type      orderpb.OrderType
	Status    orderpb.OrderStatus
	Price     decimal.Decimal
	Quantity  decimal.Decimal
	CreatedAt time.Time
}

func (o *Order) IsOwnedBy(userID string) bool {
	return o.UserID == userID
}

func (o *Order) CanBeCancelled() bool {
	return o.Status == orderpb.OrderStatus_ORDER_STATUS_CANCELLED ||
		o.Status == orderpb.OrderStatus_ORDER_STATUS_OPEN
}

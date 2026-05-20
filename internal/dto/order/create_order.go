package order

import (
	"github.com/shopspring/decimal"

	"github.com/chilly266futon/orderService/internal/domain"
)

type CreateOrderRequest struct {
	UserID    string
	MarketID  string
	OrderType domain.OrderType
	Price     decimal.Decimal
	Quantity  decimal.Decimal
}

type CreateOrderResponse struct {
	OrderID string
	Status  string
}

package order

import (
	"github.com/shopspring/decimal"
)

const (
	OrderTypeUnspecified = "UNSPECIFIED"
	OrderTypeLimit       = "LIMIT"
	OrderTypeMarket      = "MARKET"
	OrderTypeStopLimit   = "STOP_LIMIT"
	OrderTypeStopMarket  = "STOP_MARKET"
)

type CreateOrderRequest struct {
	UserID    string
	MarketID  string
	OrderType string
	Price     decimal.Decimal
	Quantity  decimal.Decimal
}

type CreateOrderResponse struct {
	OrderID string
	Status  string
}

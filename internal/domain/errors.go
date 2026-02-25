package domain

import "errors"

var (
	ErrInvalidOrderType       = errors.New("invalid order type")
	ErrInvalidOrderStatus     = errors.New("invalid order status")
	ErrInvalidPrice           = errors.New("price must be positive")
	ErrInvalidQuantity        = errors.New("quantity must be positive")
	ErrMarketNotAvailable     = errors.New("market not found or not accessible")
	ErrOrderNotFound          = errors.New("order not found")
	ErrAccessDenied           = errors.New("access denied")
	ErrOrderCannotBeCancelled = errors.New("order cannot be cancelled in current status")
	ErrOrderAlreadyCancelled  = errors.New("order is already cancelled")
)

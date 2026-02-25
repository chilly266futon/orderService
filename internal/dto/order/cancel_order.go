package order

type CancelOrderRequest struct {
	OrderID string
	UserID  string
}

type CancelOrderResponse struct {
	OrderID string
	Status  string
}

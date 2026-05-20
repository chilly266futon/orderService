package order

type GetOrderStatusRequest struct {
	OrderID string
	UserID  string
}

type GetOrderStatusResponse struct {
	OrderID string
	Status  string
}

package order

const (
	OrderStatusUnspecified = "UNSPECIFIED"
	OrderStatusCreated     = "CREATED"
	OrderStatusOpen        = "OPEN"
	OrderStatusFilled      = "FILLED"
	OrderStatusCancelled   = "CANCELLED"
	OrderStatusRejected    = "REJECTED"
)

type GetOrderStatusRequest struct {
	OrderID string
	UserID  string
}

type GetOrderStatusResponse struct {
	OrderID string
	Status  string
}

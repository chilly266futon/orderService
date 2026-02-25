package domain

type OrderStatus uint8

const (
	OrderStatusUnspecified = iota
	OrderStatusCreated
	OrderStatusOpen
	OrderStatusFilled
	OrderStatusCancelled
	OrderStatusRejected
)

func (s OrderStatus) String() string {
	switch s {
	case OrderStatusCreated:
		return "CREATED"
	case OrderStatusOpen:
		return "OPEN"
	case OrderStatusFilled:
		return "FILLED"
	case OrderStatusCancelled:
		return "CANCELLED"
	case OrderStatusRejected:
		return "REJECTED"
	default:
		return "UNSPECIFIED"
	}
}

func ParseOrderStatus(s string) (OrderStatus, error) {
	switch s {
	case "CREATED":
		return OrderStatusCreated, nil
	case "OPEN":
		return OrderStatusOpen, nil
	case "FILLED":
		return OrderStatusFilled, nil
	case "CANCELLED":
		return OrderStatusCancelled, nil
	case "REJECTED":
		return OrderStatusRejected, nil
	default:
		return OrderStatusUnspecified, ErrInvalidOrderStatus
	}
}

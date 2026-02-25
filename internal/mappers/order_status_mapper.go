package mappers

import (
	pb "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"
	"github.com/chilly266futon/orderService/internal/domain"
)

func OrderStatusToProto(s domain.OrderStatus) pb.OrderStatus {
	switch s {
	case domain.OrderStatusCreated:
		return pb.OrderStatus_ORDER_STATUS_CREATED
	case domain.OrderStatusOpen:
		return pb.OrderStatus_ORDER_STATUS_OPEN
	case domain.OrderStatusFilled:
		return pb.OrderStatus_ORDER_STATUS_FILLED
	case domain.OrderStatusCancelled:
		return pb.OrderStatus_ORDER_STATUS_CANCELLED
	case domain.OrderStatusRejected:
		return pb.OrderStatus_ORDER_STATUS_REJECTED

	default:
		return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

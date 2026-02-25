package mappers

import (
	pb "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"
	"github.com/chilly266futon/orderService/internal/domain"
)

func OrderTypeFromProto(t pb.OrderType) domain.OrderType {
	switch t {
	case pb.OrderType_ORDER_TYPE_LIMIT:
		return domain.OrderTypeLimit
	case pb.OrderType_ORDER_TYPE_MARKET:
		return domain.OrderTypeMarket
	case pb.OrderType_ORDER_TYPE_STOP_LIMIT:
		return domain.OrderTypeStopLimit
	case pb.OrderType_ORDER_TYPE_STOP_MARKET:
		return domain.OrderTypeStopMarket
	default:
		return domain.OrderTypeUnspecified
	}
}

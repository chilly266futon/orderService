package domain

type OrderType uint8

const (
	OrderTypeUnspecified = iota
	OrderTypeLimit
	OrderTypeMarket
	OrderTypeStopLimit
	OrderTypeStopMarket
)

func (t OrderType) String() string {
	switch t {
	case OrderTypeLimit:
		return "LIMIT"
	case OrderTypeMarket:
		return "MARKET"
	case OrderTypeStopLimit:
		return "STOP_LIMIT"
	case OrderTypeStopMarket:
		return "STOP_MARKET"
	default:
		return "UNSPECIFIED"
	}
}

func ParseOrderType(s string) (OrderType, error) {
	switch s {
	case "LIMIT":
		return OrderTypeLimit, nil
	case "MARKET":
		return OrderTypeMarket, nil
	case "STOP_LIMIT":
		return OrderTypeStopLimit, nil
	case "STOP_MARKET":
		return OrderTypeStopMarket, nil
	default:
		return OrderTypeUnspecified, ErrInvalidOrderType
	}
}

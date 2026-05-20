package grpc

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"
	"github.com/chilly266futon/exchange-shared/pkg/common"
	"github.com/chilly266futon/orderService/internal/domain"
	"github.com/chilly266futon/orderService/internal/dto/order"
	"github.com/chilly266futon/orderService/internal/mappers"
	"github.com/chilly266futon/orderService/internal/service"
)

type OrderServer struct {
	pb.UnimplementedOrderServiceServer
	useCase service.OrderService
}

func NewOrderServer(useCase service.OrderService) *OrderServer {
	return &OrderServer{useCase: useCase}
}

func (s *OrderServer) CreateOrder(ctx context.Context, pbReq *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	userID := common.GetUserID(ctx)
	if userID == "" {
		return nil, status.Errorf(codes.Unauthenticated, "user_id not found in context")
	}

	dtoReq := order.CreateOrderRequest{
		UserID:   userID, // user_id только из JWT
		MarketID: pbReq.MarketId,
	}

	price, err := decimal.NewFromString(pbReq.Price)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid price format: %v", err)
	}
	dtoReq.Price = price

	quantity, err := decimal.NewFromString(pbReq.Quantity)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid quantity format: %v", err)
	}
	dtoReq.Quantity = quantity

	dtoReq.OrderType = mappers.OrderTypeFromProto(pbReq.OrderType)

	dtoResp, err := s.useCase.CreateOrder(ctx, dtoReq)
	if err != nil {
		return nil, mapDomainError(err)
	}

	statusStr, err := domain.ParseOrderStatus(dtoResp.Status)
	if err != nil {
		return nil, mapDomainError(err)
	}

	return &pb.CreateOrderResponse{
		OrderId: dtoResp.OrderID,
		Status:  mappers.OrderStatusToProto(statusStr),
	}, nil
}

func (s *OrderServer) GetOrderStatus(ctx context.Context, pbReq *pb.GetOrderStatusRequest) (*pb.GetOrderStatusResponse, error) {
	dtoReq := order.GetOrderStatusRequest{
		OrderID: pbReq.OrderId,
		UserID:  pbReq.UserId,
	}

	resp, err := s.useCase.GetOrderStatus(ctx, dtoReq)
	if err != nil {
		return nil, mapDomainError(err)
	}

	statusStr, err := domain.ParseOrderStatus(resp.Status)

	return &pb.GetOrderStatusResponse{
		OrderId: resp.OrderID,
		Status:  mappers.OrderStatusToProto(statusStr),
	}, err

}

func (s *OrderServer) CancelOrder(ctx context.Context, pbReq *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	dtoReq := order.CancelOrderRequest{
		OrderID: pbReq.OrderId,
		UserID:  pbReq.UserId,
	}

	dtoResp, err := s.useCase.CancelOrder(ctx, dtoReq)
	if err != nil {
		return nil, mapDomainError(err)
	}

	statusStr, err := domain.ParseOrderStatus(dtoResp.Status)
	if err != nil {
		return nil, mapDomainError(err)
	}

	return &pb.CancelOrderResponse{
		OrderId: dtoResp.OrderID,
		Status:  mappers.OrderStatusToProto(statusStr),
	}, nil

}

func mapDomainError(err error) error {
	// Если уже gRPC status — пробрасываем как есть
	if s, ok := status.FromError(err); ok && s.Code() != codes.Unknown {
		return err
	}

	switch {
	case errors.Is(err, domain.ErrInvalidPrice),
		errors.Is(err, domain.ErrInvalidQuantity),
		errors.Is(err, domain.ErrInvalidOrderType),
		errors.Is(err, domain.ErrInvalidOrderStatus):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, domain.ErrOrderNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, domain.ErrAccessDenied):
		return status.Error(codes.PermissionDenied, err.Error())

	case errors.Is(err, domain.ErrMarketNotAvailable):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, domain.ErrActiveOrderLimitExceeded):
		return status.Error(codes.ResourceExhausted, err.Error())

	case errors.Is(err, domain.ErrOrderCannotBeCancelled),
		errors.Is(err, domain.ErrOrderAlreadyCancelled):
		return status.Error(codes.FailedPrecondition, err.Error())

	default:
		return status.Error(codes.Internal, "internal error")
	}
}

package grpc

import (
	"context"
	"errors"

	pb "github.com/chilly266futon/exchange-service-contracts/gen/pb/order"
	"github.com/chilly266futon/orderService/internal/domain"
	"github.com/chilly266futon/orderService/internal/dto/order"
	"github.com/chilly266futon/orderService/internal/mappers"
	"github.com/chilly266futon/orderService/internal/service"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type OrderServer struct {
	pb.UnimplementedOrderServiceServer
	useCase *service.OrderUseCase
}

func NewOrderServer(useCase *service.OrderUseCase) *OrderServer {
	return &OrderServer{useCase: useCase}
}

func (s *OrderServer) CreateOrder(ctx context.Context, pbReq *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	dtoReq := order.CreateOrderRequest{
		UserID:   pbReq.UserId,
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

	dtoReq.OrderType = mappers.OrderTypeFromProto(pbReq.OrderType).String()

	dtoResp, err := s.useCase.CreateOrder(ctx, dtoReq)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidPrice) || errors.Is(err, domain.ErrInvalidQuantity) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, err
	}

	statusStr, err := domain.ParseOrderStatus(dtoResp.Status)

	return &pb.CreateOrderResponse{
		OrderId: dtoResp.OrderID,
		Status:  mappers.OrderStatusToProto(statusStr),
	}, err
}

func (s *OrderServer) GetOrderStatus(ctx context.Context, pbReq *pb.GetOrderStatusRequest) (*pb.GetOrderStatusResponse, error) {
	dtoReq := order.GetOrderStatusRequest{
		OrderID: pbReq.OrderId,
		UserID:  pbReq.UserId,
	}

	resp, err := s.useCase.GetOrderStatus(ctx, dtoReq)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	statusStr, err := domain.ParseOrderStatus(dtoResp.Status)

	return &pb.CancelOrderResponse{
		OrderId: dtoResp.OrderID,
		Status:  mappers.OrderStatusToProto(statusStr),
	}, nil

}

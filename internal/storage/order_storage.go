package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chilly266futon/orderService/internal/domain"
)

type OrderStorage struct {
	db *pgxpool.Pool
}

func NewOrderStorage(db *pgxpool.Pool) *OrderStorage {
	return &OrderStorage{db: db}
}

func (s *OrderStorage) DB() *pgxpool.Pool {
	return s.db
}

func (s *OrderStorage) Create(ctx context.Context, order *domain.Order) error {
	query := `
		INSERT INTO orders (id, user_id, market_id, type, quantity, price, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := s.db.Exec(ctx, query,
		order.ID,
		order.UserID,
		order.MarketID,
		order.Type.String(),
		order.Quantity,
		order.Price,
		order.Status.String(),
		order.CreatedAt,
	)
	return err
}

func (s *OrderStorage) GetByID(ctx context.Context, orderID string) (*domain.Order, error) {
	query := `
		SELECT id, user_id, market_id, type, quantity, price, status, created_at, updated_at
		FROM orders
		WHERE id = $1`

	var typeStr, statusStr string
	order := &domain.Order{}
	err := s.db.QueryRow(ctx, query, orderID).Scan(
		&order.ID,
		&order.UserID,
		&order.MarketID,
		&typeStr,
		&order.Quantity,
		&order.Price,
		&statusStr,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrOrderNotFound
		}
		return nil, err
	}

	ot, err := domain.ParseOrderType(typeStr)
	if err != nil {
		return nil, err
	}
	order.Type = ot

	os, err := domain.ParseOrderStatus(statusStr)
	if err != nil {
		return nil, err
	}
	order.Status = os

	return order, nil
}

func (s *OrderStorage) Update(ctx context.Context, order *domain.Order) error {
	query := `
		UPDATE orders
		SET user_id = $1, market_id = $2, type = $3, quantity = $4, price = $5, status = $6, updated_at = NOW()
		WHERE id = $7`

	result, err := s.db.Exec(ctx, query,
		order.UserID,
		order.MarketID,
		order.Type.String(),
		order.Quantity,
		order.Price,
		order.Status.String(),
		order.ID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrOrderNotFound
	}
	return nil
}

func (s *OrderStorage) CountActiveByUserID(ctx context.Context, userID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM orders WHERE user_id = $1 AND status IN ($2, $3)`
	err := s.db.QueryRow(ctx, query, userID,
		domain.OrderStatusCreated.String(),
		domain.OrderStatusOpen.String(),
	).Scan(&count)
	return count, err
}

// CancelOrderAtomic atomically cancels an order if it belongs to the given user and has the expected status.
func (s *OrderStorage) CancelOrderAtomic(ctx context.Context, orderID, userID string, expectedStatus domain.OrderStatus) error {
	query := `
		UPDATE orders
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND user_id = $3 AND status = $4
	`
	result, err := s.db.Exec(ctx, query,
		domain.OrderStatusCancelled.String(),
		orderID,
		userID,
		expectedStatus.String(),
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrOrderCannotBeCancelled
	}
	return nil
}

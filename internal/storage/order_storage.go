package storage

import (
	"sync"

	"github.com/chilly266futon/orderService/internal/domain"
)

type OrderStorage struct {
	orders map[string]*domain.Order
	mu     sync.RWMutex
}

func NewOrderStorage() *OrderStorage {
	return &OrderStorage{
		orders: make(map[string]*domain.Order),
	}
}

func (s *OrderStorage) GetByID(id string) (*domain.Order, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, exists := s.orders[id]
	return order, exists
}

func (s *OrderStorage) Add(order *domain.Order) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.orders[order.ID] = order
}

func (s *OrderStorage) Update(order *domain.Order) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.orders[order.ID]; !exists {
		return false
	}

	s.orders[order.ID] = order
	return true
}

func (s *OrderStorage) GetByUserID(userID string) []*domain.Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*domain.Order, 0)
	for _, order := range s.orders {
		if order.UserID == userID {
			result = append(result, order)
		}
	}
	return result
}

func (s *OrderStorage) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.orders)
}

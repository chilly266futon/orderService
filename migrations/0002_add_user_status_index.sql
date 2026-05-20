-- +goose Up
CREATE INDEX idx_orders_user_status ON orders(user_id, status);

-- +goose Down
DROP INDEX idx_orders_user_status;

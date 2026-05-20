-- +goose Up
CREATE TABLE orders
(
    id         TEXT PRIMARY KEY,
    user_id    TEXT      NOT NULL,
    market_id  TEXT      NOT NULL,
    type       TEXT      NOT NULL,
    status     TEXT      NOT NULL,
    price      TEXT      NOT NULL,
    quantity   TEXT      NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_market_id ON orders(market_id);
CREATE INDEX idx_orders_status ON orders(status);

-- +goose Down
DROP TABLE orders;

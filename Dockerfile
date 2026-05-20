FROM golang:1.25-alpine AS builder
WORKDIR /build

# Копируем зависимости (для replace директив)
COPY exchange-shared/ exchange-shared/
COPY exchange-service-contracts/ exchange-service-contracts/

# Копируем order-service
COPY order-service/go.mod order-service/go.sum order-service/
WORKDIR /build/order-service
RUN go mod download

COPY order-service/ /build/order-service/
RUN go build -o order-service ./cmd/main.go

FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /build/order-service/order-service /app/order-service
COPY order-service/configs/config.yaml /app/config.yaml

EXPOSE 50051

ENTRYPOINT ["/app/order-service"]
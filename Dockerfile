FROM golang:1.25.5-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o order-service ./cmd/main.go

FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /app/order-service /app/order-service
COPY configs /app/configs

EXPOSE 50051

ENTRYPOINT ["/app/order-service"]
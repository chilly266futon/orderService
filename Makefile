.PHONY: build run gen test deps

gen:
	@mkdir -p gen/pb
	@find proto -name "*.proto" -exec \
		protoc \
		-I proto \
		--go_out=gen/pb \
		--go-grpc_out=gen/pb \
		--go_opt=paths=source_relative \
		--go-grpc_opt=paths=source_relative \
		{} \;

build:
	go build -v -o bin/order_service cmd/main.go

run: build
	./bin/order_service -config configs/config.yaml

test:
	go test ./... -v

deps:
	deps:
		go mod download
		go mod tidy
		go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
		go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

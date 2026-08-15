MODULE  := github.com/zxc0zxc0zxc/reconciliation
BIN     := bin
PROTO   := proto/reconciliation/v1/source.proto

.PHONY: all build test race cover vet fmt lint proto demo clean

all: fmt vet test build

build:
	go build -o $(BIN)/recon ./cmd/recon
	go build -o $(BIN)/fakesource ./examples/fakesource

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	golangci-lint run

proto:
	protoc -I proto \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		$(PROTO)

demo:
	./scripts/demo.sh

clean:
	rm -rf $(BIN) coverage.out

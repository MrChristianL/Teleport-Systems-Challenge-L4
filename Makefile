BINARIES := bin/linux/jobctl bin/linux/server
.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make build             - Build binaries"
	@echo "  make run-server        - Build and run the gRPC server"
	@echo "  make gen-proto         - Generate gRPC code from protobuf"
	@echo "  make gen-certs         - Generate TLS certificates (requires openssl)"
	@echo "  make test              - Run tests"
	@echo "  make test-race         - Run tests with race detector"
	@echo "  make clean             - Remove built binaries"

# -- cert generation --

.PHONY: gen-certs
gen-certs:
	@echo "Generating TLS certificates..."
	@./gen-certs.sh
	@echo ""
	@echo "  [DONE] TLS certificates generated in /certs directory"

# -- protoc --

.PHONY: gen-proto
gen-proto:
	@echo "Generating gRPC code from protobuf definitions..."
	@protoc \
		--proto_path=protobuf "protobuf/jobctl.proto" \
		--go_out=protobuf/v1 --go_opt=paths=source_relative \
		--go-grpc_out=protobuf/v1 --go-grpc_opt=paths=source_relative
	@echo "  [DONE] Generated gRPC code"

# -- build --

# rebuilds if any Go file in the relevant packages changes
.PHONY: build
build: $(BINARIES)

bin/linux/jobctl: $(shell find cmd/jobctl internal -name '*.go' 2>/dev/null)
	@echo "Building Client CLI tool..."
	@go build -o $@ ./cmd/jobctl
	@echo "  [DONE] $@"

bin/linux/server: $(shell find cmd/server internal -name '*.go' 2>/dev/null)
	@echo "Building gRPC Server..."
	@go build -o $@ ./cmd/server
	@echo "  [DONE] $@"

.PHONY: run-server
run-server: bin/linux/server
	@echo "Starting gRPC server..."
	@./bin/linux/server

.PHONY: clean
clean:
	@echo "Cleaning up binaries..."
	@rm -f $(BINARIES)
	@echo "  [DONE] Binaries removed"

# -- tests --

.PHONY: test
test:
	@echo "Running tests..."
	@go test -count=1 -v -cover ./internal/...

.PHONY: test-race
test-race:
	@echo "Running tests with race detector..."
	@go test -count=1 -v -race -cover ./internal/...
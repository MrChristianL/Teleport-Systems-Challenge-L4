BINARIES := bin/linux/jobctl bin/linux/server
.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make gen-proto         - Generate gRPC code from protobuf"
	@echo "  make gen-certs         - Generate TLS certificates (requires openssl)"
	@echo "  make test              - Run tests"
	@echo "  make test-race         - Run tests with race detector"

# -- proto --

.PHONY: gen-proto
gen-proto:
	@echo "Generating gRPC code from protobuf definitions..."
	@protoc \
		--proto_path=protobuf "protobuf/jobctl.proto" \
		--go_out=protobuf/v1 --go_opt=paths=source_relative \
		--go-grpc_out=protobuf/v1 --go-grpc_opt=paths=source_relative
	@echo "  [DONE] Generated gRPC code"

# -- cert generation --

.PHONY: gen-certs
gen-certs:
	@echo "Generating TLS certificates..."
	@./gen-certs.sh
	@echo ""
	@echo "  [DONE] TLS certificates generated in /certs directory"

# -- tests --

.PHONY: test
test:
	@echo "Running tests..."
	@go test -count=1 -v -cover ./internal/...

.PHONY: test-race
test-race:
	@echo "Running tests with race detector..."
	@go test -count=1 -v -race -cover ./internal/...
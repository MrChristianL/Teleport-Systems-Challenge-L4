# Remote Job Execution Service (Level 4)

A prototype remote job execution service that provides a gRPC API to run, manage, and stream output from arbitrary Linux processes.

## Documentation
- **[Design Document](./docs/design.md)**: Detailed technical specification covering gRPC streaming, mTLS security, user authorization, and process lifecycle.

## Features (Targeting Level 4)
- **mTLS Authentication**: Secure communication with TLS 1.3.
- **User Authorization**: Identity-based authorization (Admin/User roles).
- **Output Streaming**: Event-driven output streaming to multiple clients using `sync.Cond` (no polling).
- **Binary Safety**: Support for raw binary process output.
- **Concurrent Observability**: Multiple clients can stream the same job simultaneously, including full historic output replay and live update streaming.

## Makefile
The accompanying Makefile provides support for commonly utilized commands, such as building, testing, and initializing a server.

```bash
make gen-certs  # Generate client certificates
make gen-proto  # Generate .pb files

make build      # Build server binary /bin/linux/server
make run-server # Run server binary
make clean      # Delete server binary

make test       # Run tests
make test-race  # Run tests with race detector
```


## Worker Library
The Worker library provides process execution and output streaming primitives for the remote job execution service.

- **Job**: Process lifecycle management with output streaming
- **Tracker**: Job registry with unique ID generation
- **Broker**: Disk-backed output logging with `sync.Cond` coordination (internal)

### Features
- Event-driven streaming (no polling)
- Binary-safe output
- Multi-client concurrent streaming
- Complete history replay for late joiners
- Race-free offset tracking

### Testing

```bash
make test       # Run tests
make test-race  # Run tests with race detector
```
Test coverage: **87.9%**
All tests pass with race detector enabled.

Key scenarios tested:
- Concurrent readers (no lost wakeups)
- Binary data streaming
- Context cancellation
- Process lifecycle (stop vs finish)
- Concurrent job creation

## API (Server)
The gRPC server acts as the bridge between the client and the Worker library. The gRPC server is also responsible for the security of the remote job execution server, utilizing RBAC authorization and mTLS authentication.

### Features
- mTLS (TLS 1.3 enforced)
- RBAC authorization (SAN identity-derived)
- Multitenant output streaming support

### Testing
Test coverage: **72.0%**
All tests pass with race detector enabled.

Key scenarios tested:
- RPCs are limited based on user role
- TLS enforced
- Context cancellation of streams, not jobs
- Streaming live and historical output

# API (Client)
The gRPC client is the entry point for the end-user's CLI tool. It manages the mTLS handshake, credential loading, and the lifecycle of long-lived gRPC streams, ensuring secure and resilient communication with the remote service.

### Features
- Client-side RPC operators
- mTLS configuration (TLS 1.3 enforced)

### Testing
Test coverage: **77.3%**
All tests pass with race detector enabled.

Key scenarios tested:
- Non-existent job interactions
- Client response to no server
- Multi-client output streaming

## Jobctl CLI

### Features
- The `--cert` flag allows users to select which certificate they use to use when executing commands (for ease of testing)
  - Example: `./bin/linux/jobctl --cert=admin start echo hello`

- `status` and `stream` commands support funneling arguments from `start`, allowing users to avoid having to re-type the job ID when starting new jobs
  - Example: `./bin/linux/jobctl start echo hi | ./bin/linux/jobctl status`

### Testing
Test coverage: **N/A**

Testing for the CLI doesn't exist as the CLI is simply a wrapper for the client, server, and worker library. The CLI does not present any additional logic to test at this time.

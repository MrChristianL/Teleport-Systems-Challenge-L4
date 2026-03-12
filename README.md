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

## Worker Library
The Worker library provides process execution and output streaming primitives for the remote job execution service.

- **Job**: Process lifecycle management with output streaming
- **Tracker**: Job registry with unique ID generation
- **Broker**: Disk-backed output logging with sync.Cond coordination (internal)

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

Test coverage: 88.2%
All tests pass with race detector enabled.

Key scenarios tested:
- Concurrent readers (no lost wakeups)
- Binary data streaming
- Context cancellation
- Process lifecycle (stop vs finish)
- Concurrent job creation

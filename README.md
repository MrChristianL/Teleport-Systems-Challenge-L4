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

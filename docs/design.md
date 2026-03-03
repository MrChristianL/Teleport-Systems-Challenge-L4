# Scope

The remote job execution service provides process execution capabilities while supporting complete output streaming per job over a secure, authenticated connection. It is designed to manage long-running or unattended jobs on Linux systems, allowing for multi-client observability and remote command execution.

## Goals

- **CLI:** Simple CLI tool that allows users to interact with the API via easy-to-use command syntax
- **API:** gRPC-based API that allows real-time output streaming
- **Worker Library:** Reusable Go library for process management and event-driven output broadcasting
- **Security:** mTLS-only authentication with enforced minimum TLS version 1.3 requirement and SAN-based authorization
- **Streaming:** Event-driven log tailing via disk-backed output files and `sync.Cond` broadcasting.

## Out-of-scope

- **Persisted State:** Processes do not persist if the server shuts down or reboots. In the event of a server reboot, processes do not restart.
- **High Availability**: This implementation is not meant to handle extremely heavy loads, such as running a high number of jobs at a given time. While the limit of this implementation is not known, certain known trade-offs of the implementation do not scale as well as a true production implementation likely would require. These trade-offs will be discussed in greater detail throughout.
- **Resource Limitation:** Resource usage is not limited via `cgroup v2`.
- **Child Process Cleanup**: Child processes are not tracked or cleaned up via `PGID` upon job stoppage.

---

# Design Approach

## Output Streaming: Disk-backed Append-only Logging with Event-Driven Tailing

To ensure multi-client support with full historic and live output streaming, each job will write its complete output (stdout + stderr, merged) to an on-disk log file in binary chunks. For the purposes of this implementation, a chunk is defined as a single read from a job's output. Each chunk will include a 4-byte length header that details the size of each chunk, as not all chunks are assumed to be of equal size. 

For clients to effectively stream the output, the service utilizes `sync.Cond` to handle broadcasting. This allows for event-driven wakeups for tailing readers, eliminating the need for file polling or busy-waiting.

Clients that wish to stream the output of this job attach to the job’s broker and read the log from byte zero, replaying historic output. Clients track their current read position within the log, allowing the client to determine when it has caught up to the current end of file and should wait for new data. Once the client reaches EOF, it waits. If new output arrives, the broker broadcasts via `sync.Cond` to all waiting clients, waking them to read newly appended data. Since each client holds its own file handle, the service is able to support multiple concurrent readers to stream history and to receive newly broadcast live outputs independently without blocking or interfering with one another. 

For both running and finished jobs, the client reads the complete log from byte zero and streams all output. For running jobs, the stream remains open until the job finishes. For finished jobs, the stream drains the remaining output and exits immediately.

An EOF message is always sent when the stream ends, allowing clients to detect job completion without polling the job’s status. 

Overall, this approach allows for multi-client support with no polling, no busy-waiting, and no inter-client blocking.

Other benefits of this approach include:

- **Complete Output**: The log file is the single source of truth, so even late joiners get the full output.
- **Binary Safe**: No assumptions are made about the output format of any process.
- **No Per-Client Buffer Logic**: The flow of output is delegated by gRPC and the OS, so no client can block one another.

### Considerations:

- **Client Lifecycle:** Context cancellation is handled via a dedicated `done` channel on the broker. When a client disconnects or the broker closes, the waiting goroutine exits cleanly without leaking.
- **Patient Reader**: When a client reads the log file, the length header may be present before the data of the chunk has been committed to the log. To avoid this race condition, if a client encounters a length header but hits an EOF before the promised data is available, the client seeks back 4 bytes and re-enters a wait state via `cond.Wait()`. This wait loop is context-aware, ensuring the client only re-attempts the read when new data is physically committed, or the broker is closed. This method of reading ensures that either both the header and the data are read, or neither are.

### Trade-offs

- **Disk Pollution:** By storing log files of each job’s output, we run the risk of polluting and overloading the available storage on a server machine. If a job were to create massive amounts of output, that output would be stored on the log file, along with every other job’s output. While the log files are stored in `/tmp` and will be cleaned on every system reboot, this is a limitation worth addressing. A rogue job that prints infinite output rapidly could risk filling up the available disk space.
- **Future Implementations**: Additional features to improve or address current trade-offs include log rotation or truncation to reduce the threat of log files growing infinitely, or log cleanup via either manual command or a TTL-based approach.

---

# Process Lifecycle

To manage jobs and ensure proper interaction with each job, the service tracks each job with a simple status model. Jobs transition from `RUNNING` to either `FINISHED` (job completed) or `FAILED` (error or manual stop). 

## States

- `RUNNING`: Job is actively executing
- `FINISHED`: Process exited naturally with exit code 0
- `FAILED`: Process exited with non-zero exit code or was manually stopped

All state transitions are protected by `sync.RWMutex` to prevent race conditions.

## Process Termination

When a job is stopped, the service sends `SIGTERM` to allow graceful shutdown. If the process doesn’t exit within 5 seconds, the service escalates to `SIGKILL`.

## Error Information

Jobs with status `FAILED` include an exit code and error message to help diagnose what went wrong (e.g. “permission denied”, "binary does not exist”).

# API Implementation

## gRPC

gRPC is used to support output streaming from the server’s processes to the clients.

The API layer handles the implementation of our security measures, ensuring that only valid, legitimate requests find their way from the CLI to the Worker library. All RPCs return structured gRPC status codes (e.g., `Internal`, `Canceled`, `ResourceExhausted`, `PermissionDenied`) to allow the CLI to distinguish a user action from a system failure.

The API also implements mTLS and SAN-based authorization for each server/client interaction. This will be discussed in greater detail in the Security section.

## Worker

### Job

The job is the smallest building block of the overall service. Here, the service defines the actions and characteristics of a single job or process. This includes information such as the ID of the job, what argument or arguments the job calls, the job’s current status, what time the job started and stopped, the job’s exit code, and the done channel, which is closed when a job terminates.

By tracking this information for each job, clients can track individual jobs, see information on duration, exit codes, status, and more.

Jobs are started with `cmd.Start()`, as this is a non-blocking command that is capable of executing the given job’s arguments. Jobs are stopped using the graceful shutdown escalation protocol described in Process Lifecycle. 

### Broker

The broker has two main jobs: write to the log file, and publish live output to awaiting clients. This is done by signaling awaiting readers via `sync.Cond` when new data is written. Additional details regarding this feature can be found in the Output Streaming design approach.

### Tracker

The tracker is a registry of all jobs running on the server. Tracker generates and assigns job IDs for each new job, as well as adds and maintains jobs for the service. The tracker ensures no two jobs are called by the same name, and helps track each job the service creates.

---

# Security

## mTLS Authentication & TLS Configuration

To ensure authentication, this service utilizes mTLS over gRPC. To do this, both the client and server present certificates, providing mutual authentication and transport encryption.

- **TLS Configuration**: TLS 1.3 is strictly enforced, rejecting all legacy versions (TLS 1.2 and below). By design, TLS 1.3 removes vulnerable legacy ciphers and mandates modern ciphers with forward secrecy by default (e.g., `AES-256-GCM` and `ChaCha20-Poly1305`).

### Trade-offs:

- **Certificates:** For the sake of this exercise, certificates wijll be locally generated. In production, proper PKI procedures would be utilized.

---

## Subject Alternative Name (SAN) Authorization

To ensure only authorized users are able to utilize the service, the service relies upon SAN-based authorization. SAN-based authorization is a modern X.509 standard of identity management. 

During mTLS authentication, the API extracts the identity from the certificate’s SAN. These identities, for the purposes of this implementation, are either `admin`, `user`, or `unknown`. This represents the 3 core user-types that are likely to use this service. 

- **Identity Mapping:** DNS identities in each certificate map to specific roles with set permissions, effectively determining which users are authorized to execute which RPCs, such as `StartJob` or `StopJob`. As such, `admin` receives full access, `user` has read-only equivalent access, and all other users (e.g. `unknown`) have no access.

### User Authorization

| SAN Identity | Role | StartJob | StopJob | GetStatus | StreamOutput |
| --- | --- | --- | --- | --- | --- |
| admin.example.com | Admin | ✅ | ✅ | ✅ | ✅ |
| user.example.com | User | ❌ | ❌ | ✅ | ✅ |
| unknown | - | ❌ | ❌ | ❌ | ❌ |

### Considerations:

- **Simplicity over Scale:** For this challenge, SAN authorization with a hardcoded map of roles was chosen for its balance of security and simplicity.

### Trade-offs:

- **Production Alternatives:** In a large-scale production setting, alternatives like SPIFFE/SPIRE would be worth exploring further.

---

# CLI UX

Users will interact with the remote job execution service via a CLI tool (`jobctl`) to start, stop, monitor, and stream output from processes on the target Linux hosts. The CLI provides commands to manage jobs and observe their output.

## User Stories

### Story 1: Alice starts a long-running job and later checks its status

Alice wishes to start a Python script that will take several hours. She starts it and disconnects.

```bash
$ ./jobctl start python3 process_dataset.py

Job initialized
Job ID: job-8uzdiy
```

Later, Alice checks on the job to ensure it’s still running:

```bash
$ ./jobctl status job-8uzdiy

Job ID: job-8uzdiy
Status: RUNNING
Uptime: 40m 34s
```

---

### Story 2: Bob monitors output, disconnects, and reconnects later

Bob starts a database backup. He monitors it initially to ensure it starts correctly, then disconnects to check back later.

```bash
$ ./jobctl start /usr/local/bin/backup_database --follow

Job initialized
Job ID: job-2p3jgu

Beginning database backup process...
Connecting to database...
^C

# Connection closed. Job continues running.
```

Later, he reconnects from a different machine.

```bash
$ ./jobctl stream job-2p3jgu

Beginning database backup process...
Connecting to database...
Starting backup of 47 tables...
```

Bob sees the full history output plus the live updates from the job.

---

### Story 3: Multiple users monitor the same job

Carol starts the job. Dave streams the same job from his terminal. Both see history + live output.

```bash
# Carol
$ ./jobctl start --follow /usr/bin/diagnostics

Job initialized
Job ID: job-jk4s31

Starting diagnostics

# Dave (different terminal)
$ ./jobctl stream job-jk4s31

Starting diagnostics            # history
Running...                      # live updates
```

---

### Story 4: Eric stops a service

Eric starts a job, but quickly realizes he targeted the wrong process and stops the job.

```bash
$ ./jobctl start /usr/bin/incorrect.sh

Job initialized
Job ID: job-fw42gd

$ ./jobctl stop job-fw42gd

Job stopping...

$ ./jobctl status job-fw42gd

Job ID: job-fw42gd
Status: FAILED
Message: terminated (SIGTERM)
Exit Code: 143
Uptime: 0m 6s
```

---

## Failure Modes

When errors occur, the CLI provides feedback to the user with specific error messages and exit codes.

### Job binary doesn’t exist

```bash
$ ./jobctl start /usr/local/nonexistent

Error: executable not found at "/usr/local/nonexistent"
```

### Permission denied on executable

```bash
$ ./jobctl start /root/admin.sh

Error: permission denied. "/root/admin.sh" is not executable.
```

### Attempting to stop an already-finished job

```bash
$ ./jobctl status job-234efg

Job ID: job-234efg
Status: FINISHED
Message: job completed successfully
Exit code: 0
Uptime: 0m 24s

$ ./jobctl stop job-234efg

Error: [job-234efg] failed to stop: job is not running
```

---

# Constants of Implementation

Certain things are enforced as law when considering the following implementation approach. These include:

- **Append-only Job Output**: A job’s log file is strictly append-only. Existing data is never to be modified or truncated during a job’s lifecycle.
- **Reader-Write Isolation:** The producer of output never blocks reader progression. Slow or disconnected clients have zero impact on the job itself or other clients streaming output.
- **One-way State Transitions**: Job states exclusively transition forward (`RUNNING → FINISHED/FAILED` ). Once a job is terminated, it cannot return to a running state. Restarting a process is classified as a new job.
- **Chunk Completeness**: A log chunk is only considered “published” once both the length header and the data are synced to disk and the `sync.Cond` has been signaled.

---

# Edge Cases

## Process Execution

- **Process crashes immediately:** State transitions to `FAILED`, exit code captured
- **Process hangs on SIGTERM:** Graceful shutdown; `SIGTERM` escalates to `SIGKILL` after 5s
- **Binary has no execute permission:** `cmd.Start()` returns error, CLI returns a readable error to the user

## Concurrency

- **Concurrent start calls on the same job:** Both calls succeed at initializing instances of the job in question, but launch as two different instances of the process on the server and are given two different job IDs to avoid any sort of collision
- **Client disconnects mid-stream:** Job continues, other clients remain unaffected

## Output Streaming

- **Binary output:** `bytes` in ProtoBuf handles any data, no assumptions are made of the output type
- **Client joins late**: Client subscribes to a job after output has already been published to the output log file, still gets full output (historic + live) without blocking or polling

## Security

- **Expired certificate:** gRPC handshake fails before the request reaches the application
- **Unknown Identity/SAN:** Authorization check rejects with a clear error message
- **Incorrect TLS:** TLS version 1.2 and below is automatically rejected by the service
---
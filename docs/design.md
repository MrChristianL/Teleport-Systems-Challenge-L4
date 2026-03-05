# Scope

The remote job execution service provides process execution capabilities while supporting complete output streaming per job over a secure, authenticated connection. It is designed to manage long-running or unattended jobs on Linux systems, allowing for multi-client observability and remote command execution.

## Goals

- **CLI:** Simple CLI tool that allows users to interact with the API via easy-to-use command syntax
- **API:** gRPC-based API that allows real-time output streaming, mTLS authentication, and Subject Alternative Name (SAN) authorization verification
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

## Output Streaming: Disk-backed Append Logging with Event-Driven Tailing

To ensure multi-client support with full historic and live output streaming, each job will write its complete output (stdout + stderr, merged) to an on-disk log file. Instead of using discrete chunks or disk-level framing via length headers, the output is treated as a raw byte stream. This simplifies the architecture by delegating data boundary management to the gRPC and the OS.

To avoid file polling or busy-waiting, the service utilizes a `sync.Cond` broadcast mechanism. There are two parts to the success of this method: the writers and the reader.

- **The Writers**: When a job produces output, the service performs a synchronous write and `fsync` to disk. Only after the data is committed does the writer increment a global `totalWritten` byte offset and trigger `Broadcast`.
- **The Reader**: Tailing readers utilize their own independent file descriptors to read the log file. Readers track their progress using a local byte offset. When a reader encounters an `io.EOF`, it checks its local offset against the broker’s `totalWritten` value while holding a mutex. If they are equal, the reader enters a `Wait()` state, parking the goroutine until the next `Broadcast` call.

Each log file will have two concurrent writers. These exist for each job as goroutines for `stdout` and `stderr`. These goroutines publish their pipe's respective outputs to the broker. By serializing these writers, we can ensure that these writers are able to write in a single, ordered log file. This also helps to ensure that `totalWritten` remains accurate as each pipe (`cmd.StdoutPipe()` and `cmd.StderrPipe()`) streams to the broker.

### Ordering Guarantees

The previous design required a “seek-back” mechanism to handle partial writes of chunks (length headers that had yet to have accompanying data). The new byte stream design eliminates this race condition entirely.

Data is written and synced to the log file before the `totalWritten` counter is ever updated. Additionally, the `Mutex` ensures that a reader cannot enter a `Wait()` state at the exact moment the writer is updating the offset. The reader either sees the new data and continues reading, or parks and is guaranteed to be awoken by the subsequent `Broadcast` calls.

 The following describes the `Mutex` ownership between the writer and reader:

**Writers:**

1. Acquires `b.mu`
2. Writes and syncs to disk (still holding the lock in order to serialize the concurrent writers)
3. Increment `totalWritten`
4. Call `Broadcast` (valid to call while holding the lock in `sync.Cond`)
5. Releases `b.mu` via `defer`

**Reader:**

1. `f.Read(buf)` with **no lock** - this is a direct OS call where the reader manages its own position in the file independently of the writer.
2. If `Read` returns `io.EOF`, acquires `b.mu`
3. Checks the condition `!b.closed && b.totalWritten == offset` inside a `for` loop
4. Calls `b.cond.Wait()`, which atomically releases `b.mu` and parks the goroutine
5. When woken by `Broadcast`, re-acquires `b.mu` automatically before returning from `Wait`
6. Re-checks the condition (`for` loop), releases `b.mu`, and loops back to `f.Read`

The byte stream implementation provides the same benefits that the chunk-based approach previously provided, including complete output, binary safety, and no per-client buffer logic. 

## Client Lifecycle

Within the Output Streaming function, `StreamFromDisk`, there exists a watchdog goroutine that listens for either context cancellation or the broker closing. This goroutine wakes the `Wait` loop if the context is canceled before the broker closes.

The loop follows the following steps to ensure waiting clients are never left indefinitely hibernating:

1. A gRPC client disconnects, and `ctx.Done()` is closed
2. The select unblocks and acquires `b.mu`
3. It calls `Broadcast`, which wakes all goroutines waiting on `Wait`
4. The main loop (which was blocked on `Wait`) wakes up and checks `ctx.Err()`
5. Since the context is canceled, it returns `ctx.Err()`

The difference between the context cancellation and the `done` channel is that while `ctx` handles the client-side cancellation (e.g., user disconnects), the `done` channel handles the server-side completion, such as the broker closing when a job is finished. With this goroutine watching, no client is ever left waiting for a `broadcast` call that is never coming.

### **Trade-offs**

- **Memory Overload**: By storing log files of each job’s output, we run the risk of polluting the server’s storage or overwhelming the server’s available memory. Output is stored in the `/tmp` directory, meaning that log files are cleaned on every system reboot. However, since most modern Linux distributions use *tmpfs*,  `/tmp` is actually stored in memory. If a job were to create massive amounts of output, that output could deplete the server’s available memory. A rogue job printing infinite output would rapidly overload the remaining RAM of the server.
- **Future Implementations**: Additional features to improve or address current trade-offs include log rotation or truncation to reduce the threat of log files growing infinitely, or log cleanup via either manual command or a TTL-based approach.

---

# Process Lifecycle

## Process States

To manage jobs and ensure proper interaction with each job, the service tracks each job with a simple status model. Jobs transition from `RUNNING` to either `FINISHED` (job completed) or `FAILED` (error or manual stop). 

Available states include:

- `RUNNING`: Job is actively executing
- `FINISHED`: Process exited naturally with exit code 0
- `FAILED`: Process exited with non-zero exit code or was manually stopped

## Process Messages

When a job ends, it outputs with it a message that describes an explanation for the job's status (`FINISHED`/`FAILED`), and the accompanying exit code. A job that has completed naturally will have Status `FINISHED`, while a job that did not exit on its own (e.g., from an error, stopped by a user) will have Status `FAILED`.

Messages for `FINISHED` jobs will say "job completed successfully".

Messages for `FAILED` jobs will include more relevant messages, such as "stopped by user", or "general error", etc.

Messages are correlated to exit codes returned by processes. An example of such error codes and their accompanying messages includes

```bash
1:   "general error",
2:   "invalid usage or arguments",
126: "command found but cannot execute (invalid permissions or target not executable)",
127: "command not found",
137: "stopped by user (SIGKILL)"
```

In this way, the service is able to reasonably help users differentiate which jobs have stopped naturally and which have been killed by the user.

Note that for code 137, this specifies that it was stopped by the user. For the same of this implementation, a call to `stop()` will now utilize `SIGKILL` to halt a process, returning this error code.

This method of giving information to the user via both message and exit code helps to provide additional information regarding each job’s process lifecycle and status.

## Job IDs

When a job is initialized, that job is assigned a unique 6-character alphanumeric (0-9a-z) identifier. This Job ID is generated via a `generateID` function that assigns unique job IDs utilizing the `crypto/rand` library. These 6-character IDs take the form of job-XXXXXX. 

To ensure no duplicate IDs exist, the `generateID` function includes a loop that checks if that ID exists in the job tracker's map already. If it does, it generates another random ID, and will continue to do so until a new ID is generated. This occurs at the time of generation, so no job ever has the same ID. 

### Considerations

With 2.1 billion possible ID options, this approach balances CLI usability with a large enough pool to avoid ID collisions. For the sake of this challenge, this implementation offers a wide enough breadth to be usable. However, for a production implementation, UUID or ULID would likely be considered over this option due to the increased chance of collisions on high-availability systems. However, any use of UUID or ULID would still require some consideration as to how the CLI would reasonably use them as job IDs. A CLI that requires users type in 36 characters for each job is less than ideal.

# API Implementation

## gRPC

gRPC is used to support output streaming from the server’s processes to the clients.

The service exposes four RPCs that translate client requests to Worker
Library calls:

| RPC | Identity Permissions | Worker Library Mapping |
| --- | --- | --- |
| StartJob | admin | `tracker.AddJob()` → `job.Start()` |
| StopJob | admin | `tracker.GetJob()` → `job.Stop()` |
| GetStatus | admin, user | `tracker.GetJob()` → read job state |
| StreamOutput | admin, user | `tracker.GetJob()` → `job.StreamFromDisk()` |

All RPCs return structured gRPC status codes (`NotFound`, `PermissionDenied`,
`FailedPrecondition`, `Internal`, `Canceled`, `ResourceExhausted`) to allow
the CLI to distinguish user errors from system failures.

`StreamOutput()` uses server-side streaming to deliver both historical and
live output. The RPC handler calls `job.StreamFromDisk()` with a callback
that sends each chunk over the gRPC stream. When the job finishes or the
client disconnects, the gRPC stream returns `nil`, and the client reader exits.

### Security Implementation

The gRPC server enforces mTLS and identity-based authorization on every RPC:

1. TLS handshake validates client certificate
2. Extract SAN identity from certificate
3. Check if identity is authorized for the requested RPC
4. If unauthorized, return `PermissionDenied` before touching Worker Library
5. If authorized, forward request to Worker Library

For additional detail regarding the mTLS an identity-based authorization design, see the Security section.

## Worker Library Exported API

### **Tracker:**

The tracker's primary job is to keep track of all jobs executed by the service on the server. This is the entry point for job lifecycle management.

**Constructor**:

- `NewTracker() *Tracker` - This creates a new tracker instance and is used when the server is initially set up

**Methods**:

- `AddJob(command []string) (*Job, error)` - Creates and registers a new job with that job's unique ID, and feeds that job the provided command (e.g. `[]string{"python3", "script.py"}`
- `GetJob(jobID string) (*Job, error)` - Retrieves a Job reference by its ID

### **Job:**

This represents a single process execution. Jobs are returned by `GetJob()` and `AddJob()`.

**Control Methods**:

- `Start() error` - Begins job execution, initializes log writers to capture job output
- `Stop() error` - Terminates job processes via `SIGKILL`

**Query Methods**

- `Status() Status` - Returns current job state (`RUNNING`, `FINISHED`, `FAILED`)
- `ExitCode() int` - Returns the process exit code
- `CalculateRuntime() string` - Returns human-readable duration (e.g. 1m 4s)
- `StopReason() string` - Returns reason why the job stopped/failed

**Output Streaming**

- `StreamFromDisk(ctx context.Context, fn func([]byte) error) error` - Streams the complete output of the job's log file to the provided handler function. Blocks until the job finishes or the context is cancelled. Safe for any output volume. Can be called multiple times on the same job or after the job completes.

### **Broker:**

After additional consideration, the broker is now internal-only. While originally considered part of the exported API, further review shows that it does not need to be. Here's why:

- Users never instantiate the broker directly; `Job` creates it internally
- Output streaming is accessed through `Job.StreamFromDisk()`, which provides a clean public interface

---

# Security

## **mTLS Authentication & TLS Configuration**

To ensure authentication, this service utilizes mTLS over gRPC. To do this, both the client and server present certificates, providing mutual authentication and transport encryption.

- **TLS Configuration**: TLS 1.3 is strictly enforced, rejecting all legacy versions (TLS 1.2 and below). By design, TLS 1.3 removes vulnerable legacy ciphers and mandates modern ciphers with forward secrecy by default (e.g., `AES-256-GCM` and `ChaCha20-Poly1305`).

### **Trade-offs:**

- **Certificates:** For the sake of this exercise, certificates have been locally generated. In production, proper PKI procedures would be utilized.

---

## Identity-Based Authorization

To ensure secure access, the service utilizes Identity-Based Authorization tied to the mTLS handshake. Rather than relying on separate tokens or passwords, the system treats the client’s verified certificate as the sole source of identity.

### Identity Extraction

During the mTLS handshake, the service extracts the Subject Alternative Name (SAN) from the client’s X.509 certificate. For this implementation, the system recognizes three specific identities that represent the primary user archetypes:

- **`admin.example.com`**: Represents high-privilege administrative access.
- **`user.example.com`**: Represents standard read-only access.
- **`unknown.example.com`**: A placeholder identity for any certificate that does not have explicit permissions.

### Permission Mapping

A direct mapping between the verified identity and the permitted RPC actions determines authorization. While these identities currently share names with their corresponding roles for simplicity, the architecture is designed to decouple the two, allowing for future Role-Based Access Control (RBAC) expansion.

### **User Authorization**

| Identity | StartJob | StopJob | GetStatus | StreamOutput |
| --- | --- | --- | --- | --- |
| admin.example.com | ✅ | ✅ | ✅ | ✅ |
| user.example.com | ❌ | ❌ | ✅ | ✅ |
| unknown | ❌ | ❌ | ❌ | ❌ |

### **Considerations:**

- **Security by Default:** The system follows the principle of least privilege. Any identity not explicitly defined in the authorization map (such as `unknown.example.com`) is denied access to all functional RPCs by default.
- **Scalability:** For the scope of this project, a hardcoded identity map provides maximum clarity and security and limits the need for external configuration files or third-party dependencies.

### **Trade-offs:**

- **Production Alternatives:** In a large-scale production setting, alternatives like an external policy engine or SPIFFE/SPIRE would be worth exploring due to their robustness and scalability.

---

# CLI

Users will interact with the remote job execution service via a CLI tool (`jobctl`) to start, stop, monitor, and stream output from processes on the target Linux hosts. The CLI provides commands to manage jobs and observe their output.

## Commands, Subcommands, and Flags

The CLI supports the following commands:

- `start <arguments>` - Initializes a job based on the provided arguments. In this instance, arguments are any valid Linux command, such as `echo`, `ls`, `seq`, `python3`, etc.
    
    The following are valid examples of `start` command calls:
    
    1. `./jobctl start seq 1 100` - calls `seq` command, counting from 1 to 100
    2. `./jobctl start python3 [script.py](http://script.py)` - calls `python3` and runs the `script.py` file.
    
    The result of a `start` command is the corresponding job ID for the newly initialized job.
    
- `stop <job ID>` - Stops the targeted job via `SIGKILL`
- `status <job ID>` - Returns the status of the targeted job, including the job’s state (`RUNNING`, `FINISHED`, `FAILED`), uptime, and the job’s exit code and an accompanying message explain said exit code, should the job not be in the `RUNNING` state
- `stream <job ID>` - Streams the output of the targeted job as based on the content of the job’s log file, including historic and live output, as the job produces output

The CLI also includes support for the following flags:

- `--follow` - Able to be passed during a `start` call, this flag subscribes the user to stream the output of the newly initialized job immediately following `start` ; the default value for this flag is `false`
- `--cert <string>` - For ease of testing, this flag allows users of the CLI to specify which certificate they wish to utilize when running a command; the default value for this is `"admin"`
    - Available options for this flag include: `"admin"`, `“user”`, `“unknown”` . Any additionally provided input will be treated as `"unknown"`.

## User Stories

The following user stories show how one might utilize the commands and flags of the CLI.

### **Story 1: Alice starts a long-running job and later checks its status**

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

### **Story 2: Bob monitors output, disconnects, and reconnects later**

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

### **Story 3: Multiple users monitor the same job**

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

### **Story 4: Eric stops a service**

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
Message: stopped by user (SIGKILL)
Exit Code: 137
Uptime: 0m 6s
```

---

## Failure Modes

When errors occur, the CLI provides feedback to the user with specific error messages and exit codes.

### **Job binary doesn’t exist**

```bash
$ ./jobctl start /usr/local/nonexistent

Error: executable not found at "/usr/local/nonexistent"
```

### **Permission denied on executable**

```bash
$ ./jobctl start /root/admin.sh

Error: permission denied. "/root/admin.sh" is not executable.
```

### **Attempting to stop an already-finished job**

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

- **Append-only Source of Truth**: A job’s output is strictly append-only. Existing data is never to be modified or truncated during a job’s lifecycle.
- **Reader-Write Isolation:** The producer of output never blocks reader progression. Slow or disconnected clients have zero impact on the job itself or other clients streaming output. ****
- **One-way State Transitions**: Job states exclusively transition forward (`RUNNING → FINISHED/FAILED` ). Once a job is terminated, it cannot return to a running state. Restarting a process is classified as a new job.

---

# Edge Cases

## Process Execution

- **Process crashes immediately:** State transitions to `FAILED`, exit code captured
- **Binary has no execute permission:** `cmd.Start()` returns error, CLI returns a readable error to the user

## Concurrency

- **Concurrent start calls with the same command:** Both calls succeed, but launch as two separate jobs on the server and are given two different job IDs to avoid any sort of collision
- **Client disconnects mid-stream:** Job continues, other clients unaffected

## Output Streaming

- **Binary output:** `bytes` in ProtoBuf handles any data, no assumptions are made of the output type
- **Client joins late**: Client subscribes to a job after output has already been published to the output log file, still gets full output (historic + live) without blocking or polling

## Security

- **Expired certificate:** gRPC handshake fails before the request reaches the application
- **Unknown Identity:** Authorization check rejects with a clear error message

---
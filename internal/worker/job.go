/* job.go

Job defintes the actions and characteristics of a single job.
*/

package worker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

const chunkReadSize = 32 * 1024 // 32 KiB

// Status represents the current state of a job
type Status int

const (
	Running  Status = iota
	Finished        // job ended on its own
	Failed          // job exited with error
	Stopped         // job stopped by user
)

func (s Status) String() string {
	return [...]string{"RUNNING", "FINISHED", "FAILED", "STOPPED"}[s]
}

// Job represents a single process execution
type Job struct {
	ID      string
	Command []string // e.g. "python3" "script.py"

	// process management
	cmd *exec.Cmd

	// state management
	mu            sync.RWMutex
	status        Status
	exitCode      int
	stopReason    string
	stopRequested bool

	// output streaming
	broker *broker

	// job lifecycle
	done     chan struct{}
	onceDone sync.Once // to ensure cleanup occurs exactly once
}

func newJob(id string, command []string) (*Job, error) {
	if id == "" {
		return nil, fmt.Errorf("job ID cannot be empty")
	}

	if len(command) == 0 {
		return nil, fmt.Errorf("command cannot be empty")
	}

	broker, err := newBroker(id)
	if err != nil {
		return nil, fmt.Errorf("failed to create broker: %w", err)
	}

	return &Job{
		ID:      id,
		Command: command,
		broker:  broker,
		done:    make(chan struct{}),
	}, nil
}

// Start begins job execution and captures output
func (j *Job) Start() error {
	j.mu.Lock()
	if j.cmd != nil {
		j.mu.Unlock()
		return fmt.Errorf("job has already started")
	}
	j.mu.Unlock()

	cmd := exec.Command(j.Command[0], j.Command[1:]...)

	// assign broker as a single writer for both stdout and stderr
	cmd.Stdout = j.broker
	cmd.Stderr = j.broker

	// start job (non-blocking)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("[%s] failed to start job: %w", j.ID, err)
	}

	j.mu.Lock()
	j.cmd = cmd
	j.status = Running // update status after starting job
	j.mu.Unlock()

	// start monitoring job for completion
	go j.watchForFinish() // calls cmd.Wait and updates job status

	return nil
}

// Stop terminates a job via SIGKILL, blocking until process exits and returns nil if already stopped
func (j *Job) Stop() error {
	j.mu.Lock()
	if j.status != Running {
		j.mu.Unlock()
		// job already stopped, desired state achieved
		return nil
	}
	j.stopRequested = true
	process := j.cmd.Process
	j.mu.Unlock()

	process.Kill()
	<-j.done // wait for watchForFinish() to complete before returning
	return nil
}

func (j *Job) StopReason() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.stopReason
}

func (j *Job) Status() Status {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.status
}

func (j *Job) ExitCode() int {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.exitCode
}

// Streams complete output via the provided handler, blocks until the job finishes or ctx is cancelled. Supports concurrent calls.
func (j *Job) StreamFromDisk(ctx context.Context, out io.Writer) error {
	return j.broker.streamFromDisk(ctx, out)
}

func (j *Job) watchForFinish() {
	// blocks until the process exits and OS pipes are closed
	// Wait() will only return once writer pipes are drained
	err := j.cmd.Wait()

	j.mu.Lock()
	defer j.mu.Unlock()

	// determine final state based on why job ended
	if j.stopRequested {
		// use called Stop()
		j.status = Stopped
		j.stopReason = "stopped by user"

		// capture exit code for how process was terminated
		if exitErr, ok := err.(*exec.ExitError); ok {
			j.exitCode = exitErr.ExitCode()
		}
	} else if err != nil {
		// process exited with non-zero code
		j.status = Failed
		if exitErr, ok := err.(*exec.ExitError); ok {
			j.exitCode = exitErr.ExitCode()
			j.stopReason = fmt.Sprintf("process failed with exit code %d", j.exitCode)
		} else {
			j.stopReason = fmt.Sprintf("error waiting for process: %v", err)
		}
	} else {
		// process finished naturally with exit code 0
		j.status = Finished
		j.exitCode = 0
		j.stopReason = "job completed successfully"
	}

	// close broker (signals readers to stop waiting) and done channel (signals Stop() to return)
	j.onceDone.Do(func() {
		j.broker.close()
		close(j.done)
	})
}

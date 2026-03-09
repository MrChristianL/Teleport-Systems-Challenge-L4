/* job.go

Job defintes the actions and characteristics of a single job.
*/

package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

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
	mu         sync.Mutex
	status     Status
	exitCode   int
	stopReason string

	// output streaming
	broker *broker

	// job lifecycle
	done     chan struct{}
	onceDone sync.Once // to ensure cleanup occurs exactly once
}

type JobSnapshot struct {
	Status     Status
	ExitCode   int
	StopReason string
}

func newJob(id string, command []string) (*Job, error) {
	if id == "" {
		return nil, errors.New("job ID cannot be empty")
	}

	if len(command) == 0 {
		return nil, errors.New("command cannot be empty")
	}

	broker, err := newBroker(id) // initialize the job's internal broker
	if err != nil {
		return nil, fmt.Errorf("creating broker: %w", err)
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
		return errors.New("job has already started")
	}
	j.mu.Unlock()

	cmd := exec.Command(j.Command[0], j.Command[1:]...)

	// assign broker as a single writer for both stdout and stderr
	cmd.Stdout = j.broker
	cmd.Stderr = j.broker

	// start job (non-blocking)
	// cmd.Start() is considered potentially slow, so the lock is not held to avoid risking holding up other processes
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting job: %w", err)
	}

	j.mu.Lock()
	j.cmd = cmd
	j.mu.Unlock()

	// start monitoring job for completion
	go j.watchForFinish() // calls cmd.Wait and updates job status

	return nil
}

// Stop terminates a job via SIGKILL, blocking until process exits and then marking the job as Stopped
func (j *Job) Stop() error {
	j.mu.Lock()
	if j.status != Running {
		j.mu.Unlock()
		return nil
	}
	process := j.cmd.Process
	j.status = Stopped
	j.stopReason = "stopped by user"
	j.mu.Unlock()

	process.Kill()
	<-j.done
	return nil
}

func (j *Job) StopReason() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.stopReason
}

func (j *Job) Status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

func (j *Job) ExitCode() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.exitCode
}

// Snapshot provides access to multiple job traits all at once rather than forcing users to poll each individually.
// This eliminates data races where the traits may not align with one another (e.g. status and exit code don't sync up)
func (j *Job) Snapshot() JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobSnapshot{
		Status:     j.status,
		ExitCode:   j.exitCode,
		StopReason: j.stopReason,
	}
}

// Streams complete output via Writer.
// Blocks until the job finishes or ctx is cancelled and supports concurrent calls.
func (j *Job) StreamFromDisk(ctx context.Context, out io.Writer) error {
	j.mu.Lock()
	b := j.broker
	j.mu.Unlock()

	if b == nil {
		return errors.New("broker not initialized")
	}

	// No lock needed while streaming because streaming blocks and would keep other readers
	// from streaming, waiting for the lock
	return b.streamFromDisk(ctx, out)
}

func (j *Job) watchForFinish() {
	j.mu.Lock()
	cmd := j.cmd
	j.mu.Unlock()

	if cmd == nil { // if cmd is empty, there is nothing to wait for
		return
	}

	// blocks until the process exits and OS pipes are closed
	// Wait() will only return once writer pipes are drained
	err := cmd.Wait()

	// re-acquire the lock
	j.mu.Lock()
	defer j.mu.Unlock()

	// close broker (signals readers to stop waiting) and done channel (signals Stop() to return)
	defer j.onceDone.Do(func() {
		j.broker.close()
		close(j.done)
	})

	if j.status != Running {
		return
	}

	// Process exited naturally
	if err != nil {
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

}

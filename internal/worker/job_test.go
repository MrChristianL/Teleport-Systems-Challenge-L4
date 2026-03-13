package worker

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
)

// TestNewJob verifies the creation of jobs under different circumstances
func TestNewJob(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		cmd     []string
		wantErr bool
	}{
		{
			name:    "valid job creation",
			id:      "job-1",
			cmd:     []string{"echo", "hello"},
			wantErr: false,
		},
		{
			name:    "empty id should fail",
			id:      "",
			cmd:     []string{"echo", "hello"},
			wantErr: true,
		},
		{
			name:    "empty command should fail",
			id:      "job-2",
			cmd:     []string{},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job, err := newJob(test.id, test.cmd)
			if test.wantErr {
				if err == nil {
					t.Errorf("newJob() error = %v, wantErr %v", err, test.wantErr)
				}
				return
			}
			if job == nil {
				t.Errorf("expected newJob() not to be nil, wantErr %v", test.wantErr)
			}
			if job.ID != test.id {
				t.Errorf("expected ID %q, got %q", test.id, job.ID)
			}
		})
	}
}

// TestStartStopJob verifies the initialization and termination of jobs
func TestStartStopJob(t *testing.T) {
	job, err := newJob("job-3", []string{"sleep", "100"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}

	job.Start()

	status := job.Status()
	if status != Running {
		t.Errorf("Status() = %v, want Running", status)
	}

	job.Stop()

	if job.Status() != Stopped {
		t.Errorf("Status() = %v, want STOPPED", job.Status())
	}
}

// TestJobDoubleStart verifies that a job that is already running cannot be asked to run again
func TestConcurrentJobStarts(t *testing.T) {
	job, err := newJob("job-4", []string{"sleep", "5"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}
	defer job.Stop()

	var wg sync.WaitGroup
	numStarts := 3
	errCh := make(chan error, numStarts)

	// Barrier ensures all goroutines call Start() simultaneously
	var ready sync.WaitGroup
	ready.Add(numStarts)
	start := make(chan struct{})

	for i := range numStarts {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ready.Done()
			<-start // block until all goroutines are ready
			if err := job.Start(); err != nil {
				errCh <- err
			}
		}(i)
	}

	ready.Wait() // wait until all goroutines are lined up
	close(start) // release them all at once

	wg.Wait()
	close(errCh)

	errorsReceived := 0
	for range errCh {
		errorsReceived++
	}

	// Exactly 2 of the 3 concurrent starts should fail
	if errorsReceived != numStarts-1 {
		t.Errorf("expected %d errors from concurrent Start() calls, got %d", numStarts-1, errorsReceived)
	}
}

// TestJobExitCodes verifies that jobs that fail and jobs that finish exit with different exit codes
func TestJobExitCodes(t *testing.T) {
	job1, err := newJob("job-5", []string{"true"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}

	job2, err := newJob("job-6", []string{"false"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}

	job1.Start()
	job2.Start()

	// wait for jobs to conclude
	<-job1.done
	<-job2.done

	exitCode1 := job1.ExitCode()
	statusCode1 := job1.Status()

	exitCode2 := job2.ExitCode()
	statusCode2 := job2.Status()

	if exitCode1 != 0 || statusCode1 != Finished {
		t.Errorf("expected job to finish (exit code 0), got %d (%d)", statusCode1, exitCode1)
	}

	if exitCode2 == 0 || statusCode2 != Failed {
		t.Errorf("expected job to fail (exit code 1), got %d (%d)", statusCode2, exitCode2)
	}
}

// TestMultipleStops verifies that repeated stop calls succeed, as the desired state has already been met
func TestMultipleStops(t *testing.T) {
	job, err := newJob("job-7", []string{"sleep", "100"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}

	if err := job.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// First stop should succeed
	if err := job.Stop(); err != nil {
		t.Fatalf("first Stop() failed: %v", err)
	}

	// second stop should succeed - intended state already met
	if err := job.Stop(); err != nil {
		t.Errorf("expected Stop() to succeed, got: %v", err)
	}
}

// TestStopVsFinish Stop() differentiates stops requested by the user from natural exits
func TestStopVsFinish(t *testing.T) {
	// User stop
	jobStopped, _ := newJob("job-stopped", []string{"sleep", "100"})
	jobStopped.Start()
	jobStopped.Stop()
	<-jobStopped.done

	if status := jobStopped.Status(); status != Stopped {
		t.Errorf("user-stopped job: Status() = %v, want Stopped", status)
	}
	if reason := jobStopped.StopReason(); reason != "stopped by user" {
		t.Errorf("user-stopped job: StopReason() = %q, want %q", reason, "stopped by user")
	}

	// Natural finish
	jobFinished, _ := newJob("job-finished", []string{"true"})
	jobFinished.Start()
	<-jobFinished.done

	if status := jobFinished.Status(); status != Finished {
		t.Errorf("finished job: Status() = %v, want Finished", status)
	}
	if reason := jobFinished.StopReason(); reason != "job completed successfully" {
		t.Errorf("finished job: StopReason() = %q, want %q", reason, "job completed successfully")
	}
}

// TestJobOutputCapture verifies output is written to broker
func TestJobOutputCapture(t *testing.T) {
	job, _ := newJob("job-test", []string{"echo", "hello world"})
	job.Start()
	<-job.done

	// Verify broker received output
	ctx := t.Context()
	var output bytes.Buffer
	err := job.StreamFromDisk(ctx, &output)

	if err != nil {
		t.Fatalf("streamFromDisk() failed: %v", err)
	}

	if !bytes.Contains(output.Bytes(), []byte("hello world")) {
		t.Errorf("output = %q, want to contain %q", output.Bytes(), "hello world")
	}
}

// TestConcurrentStreamers verifies multiple clients can stream the same job
func TestConcurrentStreamers(t *testing.T) {
	job, _ := newJob("job-test", []string{"sh", "-c", "for i in 1 2 3; do echo line$i; sleep 0.1; done"})
	job.Start()

	// Start 3 concurrent readers
	var wg sync.WaitGroup
	results := make([][]byte, 3)

	for i := range 3 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := t.Context()
			buf := &bytes.Buffer{}
			job.StreamFromDisk(ctx, buf)
			results[idx] = buf.Bytes()
		}(i)
	}

	<-job.done
	wg.Wait()

	// All readers should see same output
	for i := range 3 {
		if !bytes.Equal(results[0], results[i]) {
			t.Errorf("reader %d output differs from reader 0", i)
		}
	}
}

// TestStreamCancellation verifies streaming stops when context is cancelled
func TestStreamCancellation(t *testing.T) {
	job, _ := newJob("job-test", []string{"sleep", "100"})
	job.Start()
	defer job.Stop()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	err := job.StreamFromDisk(ctx, &bytes.Buffer{})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("streamFromDisk() error = %v, want %v", err, context.Canceled)
	}
}

// TestConcurrentStop verifies that multiple concurrent stop calls succeed
func TestConcurrentStop(t *testing.T) {
	job, err := newJob("job-stop", []string{"sleep", "100"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}

	if err := job.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	var wg sync.WaitGroup
	const numStoppers = 5
	wg.Add(numStoppers)

	errs := make([]error, numStoppers)
	for i := range numStoppers {
		go func() {
			defer wg.Done()
			errs[i] = job.Stop()
		}()
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("stopper %d got error: %v", i, err)
		}
	}
}

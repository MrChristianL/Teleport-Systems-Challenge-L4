package worker

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"
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
			if (err != nil) != test.wantErr {
				t.Errorf("newJob() error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && job == nil {
				t.Errorf("expected newJob() not to be nil, wantErr %v", test.wantErr)
			}
			if !test.wantErr && job.ID != test.id {
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
func TestJobDoubleStart(t *testing.T) {
	job, err := newJob("job-4", []string{"sleep", "5"})
	if err != nil {
		t.Fatalf("new job failed: %v", err)
	}

	job.Start()
	defer job.Stop()

	// double start
	if err := job.Start(); err == nil {
		t.Errorf("Start() = %v, expected err != nil", err)
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
	job, err := newJob("job-8", []string{"sleep", "100"})
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

	<-job.done

	// second stop should succeed - intended state already met
	if err := job.Stop(); err != nil {
		t.Errorf("expected Stop() to succeed, got: %v", err)
	}
}

// TestStopVsFinish verifies stopRequested flag differentiates user stops from natural exits
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
	ctx := context.Background()
	var output []byte
	err := job.StreamFromDisk(ctx, func(data []byte) error {
		output = append(output, data...)
		return nil
	})

	if err != nil {
		t.Fatalf("streamFromDisk() failed: %v", err)
	}

	if !bytes.Contains(output, []byte("hello world")) {
		t.Errorf("output = %q, want to contain %q", output, "hello world")
	}
}

// TestConcurrentStreamers verifies multiple clients can stream the same job
func TestConcurrentStreamers(t *testing.T) {
	job, _ := newJob("job-test", []string{"sh", "-c", "for i in 1 2 3; do echo line$i; sleep 0.1; done"})
	job.Start()

	// Start 3 concurrent readers
	var wg sync.WaitGroup
	results := make([][]byte, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()
			job.StreamFromDisk(ctx, func(data []byte) error {
				results[idx] = append(results[idx], data...)
				return nil
			})
		}(i)
	}

	<-job.done
	wg.Wait()

	// All readers should see same output
	for i := 1; i < 3; i++ {
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

	ctx, cancel := context.WithCancel(context.Background())

	streamDone := make(chan error, 1)
	go func() {
		streamDone <- job.StreamFromDisk(ctx, func(data []byte) error {
			return nil
		})
	}()

	// Cancel after 100ms
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Stream should exit quickly
	select {
	case err := <-streamDone:
		if err != context.Canceled {
			t.Errorf("streamFromDisk() error = %v, want context.Canceled", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("streamFromDisk() did not exit after context cancellation")
	}
}

package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestStartandGetStatus verifies the StartJob and GetStatus RPCs
func TestStartAndGetStatus(t *testing.T) {
	// start a server as admin
	client := createNewServerSingleClient(t, "admin")

	job, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"true"},
	})

	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	resp, err := client.GetStatus(t.Context(), &pb.GetStatusRequest{
		JobId: job.JobId,
	})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if resp.Status != pb.GetStatusResponse_FINISHED {
		t.Errorf("expected status FINISHED, got %s", resp.Status)
	}

	if resp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", resp.ExitCode)
	}
}

// TestStopJob verifies the StopJob RPC and that stopped jobs return
// status: STOPPED
func TestStopJob(t *testing.T) {
	client := createNewServerSingleClient(t, "admin")

	// start a long-running job
	startResp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"sleep", "100"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// stop the job
	stopResp, err := client.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: startResp.JobId,
	})
	if err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}
	if !stopResp.Success {
		t.Fatalf("StopJob returned success=false: %s", stopResp.Message)
	}

	statusResp, err := client.GetStatus(t.Context(), &pb.GetStatusRequest{
		JobId: startResp.JobId,
	})

	if statusResp.Status != pb.GetStatusResponse_STOPPED {
		t.Errorf("expected STOPPED after Stop(), got %s", statusResp)
	}

}

// TestMultipleClients verifies numerous clients can stream output at a time
// and that unknown users are unable to stream output
func TestMultipleClients(t *testing.T) {
	clients := createNewServerMultipleClients(t, "admin", "user", "unknown")

	admin, user, unknown := clients[0], clients[1], clients[2]

	startResp, err := admin.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"echo", "-n", "hello world"},
	})
	if err != nil {
		t.Fatalf("failed to StartJob: %v", err)
	}

	// capture output and err for collectStream helper function
	type result struct {
		out string
		err error
	}
	adminResCh := make(chan result, 1)
	userResCh := make(chan result, 1)

	go func() {
		out, err := collectStream(t.Context(), admin, startResp.JobId)
		adminResCh <- result{out, err}
	}()
	go func() {
		out, err := collectStream(t.Context(), user, startResp.JobId)
		userResCh <- result{out, err}
	}()

	// Handle the 'unknown' client (expected to fail)
	// We do this in the main thread to keep the logic clear
	_, recvErr := collectStream(t.Context(), unknown, startResp.JobId)

	if recvErr == nil {
		t.Errorf("expected error for unknown user, got nil")
	} else {
		st, ok := status.FromError(recvErr)
		if !ok || st.Code() != codes.Unauthenticated {
			t.Errorf("expected code Unauthenticated, got %v (%v)", st.Code(), recvErr)
		}
	}

	// collect and validate concurrent results
	resAdmin := <-adminResCh
	resUser := <-userResCh

	if resAdmin.err != nil {
		t.Errorf("admin stream failed: %v", resAdmin.err)
	}
	if resUser.err != nil {
		t.Errorf("user stream failed: %v", resUser.err)
	}

	if resAdmin.out != "hello world" {
		t.Errorf("expected 'hello world', got %q", resAdmin.out)
	}

	if resAdmin.out != resUser.out {
		t.Errorf("outputs mismatch!\nAdmin: %q\nUser:  %q", resAdmin.out, resUser.out)
	}
}

// TestStreamLiveOutput verifies that clients can stream output as it is published to disk
func TestStreamLiveOutput(t *testing.T) {
	client := createNewServerSingleClient(t, "admin")

	// start a job that takes long enough to stream while running
	startResp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"seq", "1", "10000"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// subscribe immediately — job should still be running
	stream, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: startResp.JobId,
	})
	if err != nil {
		t.Fatalf("StreamOutput failed: %v", err)
	}

	var got []byte
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}
		got = append(got, resp.Chunk...)
	}

	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 10000 {
		t.Errorf("expected 10000 lines, got %d", len(lines))
	}
	if lines[0] != "1" {
		t.Errorf("expected first line '1', got %q", lines[0])
	}
	if lines[9999] != "10000" {
		t.Errorf("expected last line '10000', got %q", lines[9999])
	}
}

func TestStreamLateSubscriber(t *testing.T) {
	client := createNewServerSingleClient(t, "admin")

	startResp, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"seq", "1", "500"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// Active polling: wait up to 2 seconds for the job to finish.
	pollCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	for {
		statusResp, err := client.GetStatus(pollCtx, &pb.GetStatusRequest{
			JobId: startResp.JobId,
		})
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}

		if statusResp.Status != pb.GetStatusResponse_RUNNING {
			break // Job is finished, exit the loop
		}

		select {
		case <-pollCtx.Done():
			t.Fatalf("timed out waiting for job to finish")

		// 10ms delay between polling to keep from spamming the server with requests
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The job is definitively finished. Connect as the late subscriber.
	stream, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: startResp.JobId,
	})
	if err != nil {
		t.Fatalf("StreamOutput failed: %v", err)
	}

	var got []byte
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}
		got = append(got, resp.Chunk...)
	}

	// seq 1 500 produces "1\n2\n...500\n"
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 500 {
		t.Errorf("expected 500 lines, got %d", len(lines))
	}

	if lines[0] != "1" {
		t.Errorf("expected first line to be '1', got %q", lines[0])
	}

	if lines[499] != "500" {
		t.Errorf("expected last line to be '500', got %q", lines[499])
	}
}

// TestStopNonExistentJob verifies stopping a non-existent job returns an error
func TestStopNonExistentJob(t *testing.T) {
	client := createNewServerSingleClient(t, "admin")

	jobID := "job-123abc"

	// stop job that doesn't exist
	stopResp, err := client.StopJob(t.Context(), &pb.StopJobRequest{
		JobId: jobID,
	})
	if err != nil {
		t.Fatalf("StopJob returned error: %v", err)
	}

	if stopResp.Success {
		t.Errorf("expected Success=false for non-existent job, got true")
	}

	expectedMsg := fmt.Sprintf("[%s]: job not found", jobID)

	if stopResp.Message != expectedMsg {
		t.Errorf("expected message %q, got %q", expectedMsg, stopResp.Message)
	}
}

// TestGetStatusNonExistentJob verifies GetStatus returns UNKNOWN for a non-existent job
func TestGetStatusNonExistentJob(t *testing.T) {
	client := createNewServerSingleClient(t, "user")

	jobID := "job-123abc"

	// get status for job that doesn't exist
	statusResp, err := client.GetStatus(t.Context(), &pb.GetStatusRequest{
		JobId: jobID,
	})

	if err != nil {
		t.Fatalf("GetStatus returned err: %v", err)
	}

	if statusResp.Status != pb.GetStatusResponse_UNKNOWN {
		t.Errorf("expected status UNKNOWN, got %q", statusResp.Status)
	}

	if statusResp.ExitCode != -1 {
		t.Errorf("expected exit code -1, got %d", statusResp.ExitCode)
	}
}

// TestStreamOutputNonExistentJob verifies StreamOutput returns error for non-existent jobs
func TestStreamOutputNonExistentJob(t *testing.T) {
	client := createNewServerSingleClient(t, "user")

	jobID := "job-123abc"

	stream, err := client.StreamOutput(t.Context(), &pb.StreamOutputRequest{
		JobId: jobID,
	})
	if err != nil {
		t.Fatalf("StreamOutput returned error: %v", err)
	}

	// The error should occur when trying to receive from the stream
	_, err = stream.Recv()
	if err == nil {
		t.Fatalf("expected error when receiving from stream for non-existent job, got nil")
	}

	// The error message should contain "job not found"
	expectedErrSubstring := "job not found"
	if !strings.Contains(err.Error(), expectedErrSubstring) {
		t.Errorf("expected error to contain %q, got %q", expectedErrSubstring, err.Error())
	}
}

// TestStartJobBadCommand verifies StartJob returns an error for invalid commands.
// The Python3 command is invalid in this case because we do not have that script present
func TestStartJobBadCommand(t *testing.T) {
	client := createNewServerSingleClient(t, "admin")

	// start a job
	job, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"python3", "script.py"}, // attempt to run a script we do not  have
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	if job.JobId == "" {
		t.Fatal("expected non-empty job ID")
	}

	// Active polling: wait up to 2 seconds for the job to fail
	pollCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	for {
		statusResp, err := client.GetStatus(pollCtx, &pb.GetStatusRequest{
			JobId: job.JobId,
		})
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}

		if statusResp.Status != pb.GetStatusResponse_RUNNING {
			break // Job is finished, exit the loop
		}

		select {
		case <-pollCtx.Done():
			t.Fatalf("timed out waiting for job to finish")

		case <-time.After(10 * time.Millisecond):
			// Continue to next iteration
		}
	}

	// check status of job -- should have failed
	statusResp, err := client.GetStatus(t.Context(), &pb.GetStatusRequest{
		JobId: job.JobId,
	})
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if statusResp.Status != pb.GetStatusResponse_FAILED {
		t.Errorf("expected status FAILED, got %s", statusResp.Status)
	}

	if statusResp.ExitCode == 0 {
		t.Errorf("expected non-zero exit code for failed job, got 0")
	}
}

// TestStartJobNonExistentBinary verifies invalid StartJob return error when targeting non-existant binaries
func TestStartJobNonExistentBinary(t *testing.T) {
	client := createNewServerSingleClient(t, "admin")

	_, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"/usr/local/nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent binary, got nil")
	}

	s, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if s.Code() != codes.Internal {
		t.Errorf("expected codes.Internal, got %v", s.Code())
	}
	if !strings.Contains(s.Message(), "executable not found") {
		t.Errorf("expected 'executable not found' in message, got %q", s.Message())
	}
}

// TestStreamCancellation verifies clients can cancel streams without canceling full jobs
func TestStreamCancellation(t *testing.T) {
	client := createNewServerSingleClient(t, "admin")

	job, err := client.StartJob(t.Context(), &pb.StartJobRequest{
		Command: []string{"sleep", "10"},
	})
	if err != nil {
		t.Fatalf("StartJob(): %v", err)
	}

	// Create a cancellable context specifically for the stream
	streamCtx, cancelStream := context.WithCancel(t.Context())

	stream, err := client.StreamOutput(streamCtx, &pb.StreamOutputRequest{
		JobId: job.JobId,
	})
	if err != nil {
		t.Fatalf("StreamOutput(): %v", err)
	}

	// active polling for up to 2 seconds to ensure job is Running
	pollCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	for {
		statusResp, err := client.GetStatus(pollCtx, &pb.GetStatusRequest{
			JobId: job.JobId,
		})
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		if statusResp.Status == pb.GetStatusResponse_RUNNING {
			break // Job is Running, exit loop
		}

		select {
		case <-pollCtx.Done():
			t.Fatalf("timed out waiting for job to finish")
		case <-time.After(10 * time.Millisecond):
			// Continue to next iteration
		}
	}

	cancelStream()

	// Verify the stream returns a cancellation error
	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("expected error after context cancel, got nil")
	}
	st, ok := status.FromError(recvErr)
	if !ok || (st.Code() != codes.Canceled && st.Code() != codes.Unavailable) {
		t.Errorf("expected Canceled or Unavailable, got %v", st.Code())
	}

	// The job itself should still be running — canceling the stream is not stopping the job
	jobStatus, err := client.GetStatus(t.Context(), &pb.GetStatusRequest{
		JobId: job.JobId,
	})
	if err != nil {
		t.Fatalf("GetStatus(): %v", err)
	}
	if jobStatus.Status != pb.GetStatusResponse_RUNNING {
		t.Errorf("expected job still RUNNING after stream cancel, got %s", jobStatus.Status)
	}
}

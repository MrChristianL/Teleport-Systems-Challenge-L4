package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// func TestStartAndGetStatus(t *testing.T) {
// 	// start a server as admin
// 	client, cleanup := createNewServerSingleClient(t, "admin")
// 	defer cleanup()

// 	// start a job
// 	job, err := client.StartJob(context.Background(), &pb.StartJobRequest{
// 		Command: []string{"echo", "hello"},
// 	})
// 	if err != nil {
// 		t.Fatalf("StartJob failed: %v", err)
// 	}

// 	if job.JobId == "" {
// 		t.Fatal("expected non-empty job ID")
// 	}

// 	// immediate check should show job as RUNNING
// 	firstStatus, err := client.GetStatus(context.Background(), &pb.GetStatusRequest{
// 		JobId: job.JobId,
// 	})
// 	if err != nil {
// 		t.Fatalf("GetStatus failed: %v", err)
// 	}

// 	if firstStatus.Status != pb.GetStatusResponse_RUNNING {
// 		t.Errorf("expected status RUNNING, got %s", firstStatus.Status)
// 	}

// 	// wait for job to finish and check status
// 	time.Sleep(100 * time.Millisecond)
// 	secondStatus, err := client.GetStatus(context.Background(), &pb.GetStatusRequest{
// 		JobId: job.JobId,
// 	})
// 	if err != nil {
// 		t.Fatalf("GetStatus failed: %v", err)
// 	}

// 	if secondStatus.Status != pb.GetStatusResponse_FINISHED {
// 		t.Errorf("expected status FINISHED, got %s", secondStatus.Status)
// 	}

// 	if secondStatus.ExitCode != 0 {
// 		t.Errorf("expected exit code 0, got %d", secondStatus.ExitCode)
// 	}
// }

func TestStopJob(t *testing.T) {
	client, cleanup := createNewServerSingleClient(t, "admin")
	defer cleanup()

	// start a long-running job
	startResp, err := client.StartJob(context.Background(), &pb.StartJobRequest{
		Command: []string{"sleep", "100"},
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	// stop the job
	stopResp, err := client.StopJob(context.Background(), &pb.StopJobRequest{
		JobId: startResp.JobId,
	})
	if err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}
	if !stopResp.Success {
		t.Fatalf("StopJob returned success=false: %s", stopResp.Message)
	}

	// poll status until job is no longer running using waitgroup
	var finalStatus *pb.GetStatusResponse
	statusChan := make(chan *pb.GetStatusResponse, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			status, err := client.GetStatus(context.Background(), &pb.GetStatusRequest{
				JobId: startResp.JobId,
			})
			if err != nil {
				t.Errorf("GetStatus failed: %v", err)
				return
			}

			if status.Status != pb.GetStatusResponse_RUNNING {
				statusChan <- status
				return
			}

			select {
			case <-ticker.C:
				continue
			case <-ctx.Done():
				return
			}
		}
	}()

	select {
	case finalStatus = <-statusChan:
		// Got the final status
	case <-ctx.Done():
		t.Fatalf("timeout waiting for job to stop")
	}

	wg.Wait()

	if finalStatus.Status != pb.GetStatusResponse_STOPPED {
		t.Errorf("expected STOPPED after stop, got %s", finalStatus.Status)
	}

	if finalStatus.ExitCode == 0 {
		t.Errorf("expected non-zero exit code after SIGKILL, got 0")
	}
}

// func TestMultipleClients(t *testing.T) {
// 	clients, cleanup := createNewServerMultipleClients(t, "admin", "user", "unknown")
// 	defer cleanup()

// 	admin := clients[0]
// 	user := clients[1]
// 	unknown := clients[2]

// 	// admin starts the job
// 	startResp, err := admin.StartJob(context.Background(), &pb.StartJobRequest{
// 		Command: []string{"echo", "hello world"},
// 	})
// 	if err != nil {
// 		t.Fatalf("failed to StartJob: %v", err)
// 	}

// 	// all clients stream output concurrently
// 	adminCh := make(chan string, 1)
// 	userCh := make(chan string, 1)

// 	go func() { adminCh <- collectStream(t, admin, startResp.JobId) }()
// 	go func() { userCh <- collectStream(t, user, startResp.JobId) }()

// 	// unknown cannot use collectStream.
// 	// unknown should get permission denied when trying to streamOutput
// 	unknownStream, err := unknown.StreamOutput(context.Background(), &pb.StreamOutputRequest{
// 		JobId: startResp.JobId,
// 	})
// 	if err != nil {
// 		t.Fatalf("StreamOutput returned error: %v", err)
// 	}

// 	outAdmin := <-adminCh
// 	outUser := <-userCh

// 	// unknown should get permission denied error when trying to receive
// 	var got []byte
// 	var recvErr error
// 	for {
// 		resp, err := unknownStream.Recv()
// 		if err != nil {
// 			// Expected: permission denied error
// 			recvErr = err
// 			break
// 		}
// 		got = append(got, resp.Chunk...)
// 	}
// 	outUnknown := string(got)

// 	// unknown should get get a Permission Denied error since it has no authorized access
// 	if recvErr == nil {
// 		t.Errorf("expected permission denied error for unknown user, got nil")
// 	} else {
// 		st, ok := status.FromError(recvErr)
// 		if !ok {
// 			t.Errorf("error is not a gRPC status: %v", recvErr)
// 		} else if st.Code() != codes.PermissionDenied {
// 			t.Errorf("expected code %v, got %v", codes.PermissionDenied, st.Code())
// 		}
// 	}

// 	if outAdmin != outUser {
// 		t.Errorf("client outputs do not match:\nadmin: %q\nuser: %q\nunknown: %q", outAdmin, outUser, outUnknown)
// 	}

// 	if outAdmin == "" {
// 		t.Errorf("expected output, got empty")
// 	}

// 	if outUnknown != "" {
// 		t.Errorf("unknown user output should be empty, got non-empty output")
// 	}
// }

// func TestStreamLiveOutput(t *testing.T) {
// 	client, cleanup := createNewServerSingleClient(t, "admin")
// 	defer cleanup()

// 	// start a job that takes long enough to stream while running
// 	startResp, err := client.StartJob(context.Background(), &pb.StartJobRequest{
// 		Command: []string{"seq", "1", "10000"},
// 	})
// 	if err != nil {
// 		t.Fatalf("StartJob failed: %v", err)
// 	}

// 	// subscribe immediately — job should still be running
// 	stream, err := client.StreamOutput(context.Background(), &pb.StreamOutputRequest{
// 		JobId: startResp.JobId,
// 	})
// 	if err != nil {
// 		t.Fatalf("StreamOutput failed: %v", err)
// 	}

// 	var got []byte
// 	for {
// 		resp, err := stream.Recv()
// 		if err != nil {
// 			break
// 		}
// 		got = append(got, resp.Chunk...)
// 	}

// 	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
// 	if len(lines) != 10000 {
// 		t.Errorf("expected 10000 lines, got %d", len(lines))
// 	}
// 	if lines[0] != "1" {
// 		t.Errorf("expected first line '1', got %q", lines[0])
// 	}
// 	if lines[9999] != "10000" {
// 		t.Errorf("expected last line '10000', got %q", lines[9999])
// 	}
// }

// func TestStreamLargeOutputWithLateSubscriber(t *testing.T) {
// 	client, cleanup := createNewServerSingleClient(t, "admin")
// 	defer cleanup()

// 	startResp, err := client.StartJob(context.Background(), &pb.StartJobRequest{
// 		Command: []string{"seq", "1", "500"},
// 	})
// 	if err != nil {
// 		t.Fatalf("StartJob failed: %v", err)
// 	}

// 	// wait for job to finish, then verify late subscriber still gets full output
// 	time.Sleep(200 * time.Millisecond)

// 	stream, err := client.StreamOutput(context.Background(), &pb.StreamOutputRequest{
// 		JobId: startResp.JobId,
// 	})
// 	if err != nil {
// 		t.Fatalf("StreamOutput failed: %v", err)
// 	}

// 	var got []byte
// 	for {
// 		resp, err := stream.Recv()
// 		if err != nil {
// 			break
// 		}
// 		got = append(got, resp.Chunk...)
// 	}

// 	// seq 1 500 produces "1\n2\n...500\n"
// 	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
// 	if len(lines) != 500 {
// 		t.Errorf("expected 500 lines, got %d", len(lines))
// 	}

// 	if lines[0] != "1" {
// 		t.Errorf("expected first line to be '1', got %q", lines[0])
// 	}

// 	if lines[499] != "500" {
// 		t.Errorf("expected last line to be '500', got %q", lines[499])
// 	}
// }

func TestStopNonExistentJob(t *testing.T) {
	client, cleanup := createNewServerSingleClient(t, "admin")
	defer cleanup()

	jobID := "job-123abc"

	// stop job that doesn't exist
	stopResp, err := client.StopJob(context.Background(), &pb.StopJobRequest{
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

func TestGetStatusNonExistentJob(t *testing.T) {
	client, cleanup := createNewServerSingleClient(t, "user")
	defer cleanup()

	jobID := "job-123abc"

	// get status for job that doesn't exist
	statusResp, err := client.GetStatus(context.Background(), &pb.GetStatusRequest{
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

func TestStreamOutputNonExistentJob(t *testing.T) {
	client, cleanup := createNewServerSingleClient(t, "user")
	defer cleanup()

	jobID := "job-123abc"

	stream, err := client.StreamOutput(context.Background(), &pb.StreamOutputRequest{
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

func TestStartJobBadCommmand(t *testing.T) {
	client, cleanup := createNewServerSingleClient(t, "admin")
	defer cleanup()

	// start a job
	job, err := client.StartJob(context.Background(), &pb.StartJobRequest{
		Command: []string{"python3", "script.py"}, // attempt to run a script we do not  have
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	if job.JobId == "" {
		t.Fatal("expected non-empty job ID")
	}

	// wait for fast-failing job to exit
	time.Sleep(200 * time.Millisecond)

	// check status of job -- should have failed
	statusResp, err := client.GetStatus(context.Background(), &pb.GetStatusRequest{
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

func TestStartJobNonExistentBinary(t *testing.T) {
	client, cleanup := createNewServerSingleClient(t, "admin")
	defer cleanup()

	_, err := client.StartJob(context.Background(), &pb.StartJobRequest{
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

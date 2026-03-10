package client

import (
	"context"
	"fmt"
	"testing"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"
)

func TestClientStartStopJob(t *testing.T) {
	certPrefix := "admin"
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, certPrefix)
	defer closeClient()

	respID, err := client.StartJob(context.Background(), []string{"sleep", "100"})
	if err != nil {
		t.Fatalf("failed to StartJob: %v", err)
	}
	if respID == "" {
		t.Errorf("expected non-empty job ID, got empty")
	}

	stopResp, stopMsg, err := client.StopJob(context.Background(), respID)

	if err != nil {
		t.Fatalf("failed to StopJob: %v", err)
	}
	if !stopResp {
		t.Errorf("expected StopJob to succeed, got false %s: %v", certPrefix, err)
	}
	if stopMsg == "" {
		t.Errorf("expected message output, got nil")
	}
}

func TestClientGetStatus(t *testing.T) {
	certPrefix := "admin"
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, certPrefix)
	defer closeClient()

	respID, err := client.StartJob(context.Background(), []string{"echo", "test"})
	if err != nil {
		t.Fatalf("failed to StartJob: %v", err)
	}

	// message doesn't exist for in-progress jobs; message is used as an explaination of exit code
	status, exitCode, _, err := client.GetStatus(context.Background(), respID)
	if err != nil {
		t.Fatalf("failed to GetStatus: %v", err)
	}
	if status == pb.GetStatusResponse_UNKNOWN {
		t.Errorf("status expected to be FINISHED, got UNKNOWN")
	}
	if exitCode == -1 { // -1 is unknown/error exit code
		t.Errorf("expected exitCode 0, got -1")
	}
}

func TestClientStreamOutput(t *testing.T) {
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, "admin")
	defer closeClient()

	respID, err := client.StartJob(context.Background(), []string{"echo", "test"})
	if err != nil {
		t.Fatalf("failed to StartJob: %v", err)
	}

	var streamResp []byte
	handlerCalled := false

	err = client.StreamOutput(context.Background(), respID, func(chunk []byte) error {
		handlerCalled = true
		streamResp = append(streamResp, chunk...)
		return nil
	})

	if err != nil {
		t.Fatalf("failed to StreamOutput: %v", err)
	}

	if !handlerCalled {
		t.Error("handler was never invoked")
	}

	if len(streamResp) == 0 {
		t.Error("expected to receive output, got empty")
	}
}

func TestMultipleClients(t *testing.T) {
	addr, stopServer := startTestServer(t)
	defer stopServer()

	admin, closeAdminClient := newTestClient(t, addr, "admin")
	defer closeAdminClient()

	user, closeUserClient := newTestClient(t, addr, "user")
	defer closeUserClient()

	unknown, closeUnknownClient := newTestClient(t, addr, "unknown")
	defer closeUnknownClient()

	respID, err := admin.StartJob(context.Background(), []string{"seq", "1", "1000"}) // long running job, subscribers will join in the middle
	if err != nil {
		t.Fatalf("failed to StartJob: %v", err)
	}

	// unknown receives no response as it is rejected immediately

	adminCh := make(chan []byte, 1)
	userCh := make(chan []byte, 1)

	if err = unknown.StreamOutput(context.Background(), respID, func(chunk []byte) error {
		return nil
	}); err == nil {
		t.Errorf("unknown StreamOutput should receive code PermissionDenied, got nil: %v", err)
	}

	go func() {
		var buffer []byte
		admin.StreamOutput(context.Background(), respID, func(chunk []byte) error {
			buffer = append(buffer, chunk...)
			return nil
		})
		adminCh <- buffer
	}()

	go func() {
		var buffer []byte
		user.StreamOutput(context.Background(), respID, func(chunk []byte) error {
			buffer = append(buffer, chunk...)
			return nil
		})
		userCh <- buffer
	}()

	adminResp := <-adminCh
	userResp := <-userCh

	if len(adminResp) == 0 {
		t.Errorf("admin rceived no output")
	}

	if len(adminResp) != len(userResp) {
		t.Errorf("streamed output for admin and user should be identical: admin=%d user=%d", len(adminResp), len(userResp))
	}

}

func TestClientStopNonExistentJob(t *testing.T) {
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, "admin")
	defer closeClient()

	jobID := "job-123abc"
	success, message, err := client.StopJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("StopJob failed")
	}

	if success {
		t.Errorf("StopJob exected to fail, got success")
	}

	expectedMsg := fmt.Sprintf("[%s]: job not found", jobID)
	if message != expectedMsg {
		t.Errorf("message expected to be empty, got %q", message)
	}
}

func TestClientGetStatusNonExistentJob(t *testing.T) {
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, "admin")
	defer closeClient()

	jobID := "job-123abc"
	status, exitCode, message, err := client.GetStatus(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to GetStatus: %v", err)
	}

	if status != pb.GetStatusResponse_UNKNOWN {
		t.Errorf("expected status UNKNOWN, got %d", status)
	}

	if exitCode != -1 {
		t.Errorf("expected error code -1, got %d", exitCode)
	}

	expectedMsg := fmt.Sprintf("[%s]: job not found", jobID)
	if message != expectedMsg {
		t.Errorf("expected job not found message, got %s", message)
	}
}

func TestStreamNonExistentJob(t *testing.T) {
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, "admin")
	defer closeClient()

	jobID := "job-123abc"
	var handlerCalled bool
	var streamResp []byte
	var receivedEOF bool
	err := client.StreamOutput(context.Background(), jobID, func(chunk []byte) error {
		handlerCalled = true
		streamResp = append(streamResp, chunk...)
		return nil
	})

	if err == nil {
		t.Fatalf("expected StreamOutput to fail, got nil")
	}

	if handlerCalled {
		t.Error("expected request to never handler, but handler function was called")
	}

	if receivedEOF {
		t.Error("EOF flag shouldn't have been sent as the StreamOutput call is never returned, but got true")
	}

	if len(streamResp) != 0 {
		t.Errorf("expected to receive no output, got %s", string(streamResp))
	}
}

func TestClientStartEmptyCommand(t *testing.T) {
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, "admin")
	defer closeClient()

	respID, err := client.StartJob(context.Background(), []string{})
	if err == nil {
		t.Fatalf("expected StartJob to fail, got: %v", err)
	}

	if respID != "" {
		t.Errorf("expected job ID to be empty, got %s", respID)
	}

}

func TestClientNoServer(t *testing.T) {
	ipAddr := "127.0.0.1:0"
	client, closeClient := newTestClient(t, ipAddr, "admin")
	defer closeClient()

	_, err := client.StartJob(context.Background(), []string{"sleep", "1"})
	if err == nil {
		t.Fatalf("expected StartJob to fail, got %v", err)
	}
}

func TestDuplicateStopJob(t *testing.T) {
	addr, stopServer := startTestServer(t)
	defer stopServer()

	client, closeClient := newTestClient(t, addr, "admin")
	defer closeClient()

	jobID, err := client.StartJob(context.Background(), []string{"sleep", "100"})
	if err != nil {
		t.Fatalf("failed to StartJob: %v", err)
	}

	// first stop request
	firstResp, firstMsg, err := client.StopJob(context.Background(), jobID)

	if err != nil {
		t.Fatalf("failed to StopJob: %v", err)
	}
	if !firstResp {
		t.Errorf("expected StopJob to succeed, got false: %v", err)
	}
	if firstMsg == "" {
		t.Errorf("expected message output, got nil")
	}

	// second stop request - should fail
	secondResp, secondMsg, err := client.StopJob(context.Background(), jobID)

	if err != nil {
		t.Fatalf("StopJob failed: %v", err)
	}
	if !secondResp {
		t.Errorf("expected duplicate StopJob to return true, got false: %v", err)
	}

	if secondMsg != "Success: job stopped" {
		t.Errorf("expected 'Success: job stopped', got %q", secondMsg)
	}
}

package server

import (
	"testing"
)

func TestIsAuthorized(t *testing.T) {
	tests := []struct {
		name     string
		sans     []string
		method   string
		wantAuth bool
	}{
		{"admin can start job", []string{"admin"}, "/jobctl.JobService/StartJob", true},
		{"admin can stop job", []string{"admin"}, "/jobctl.JobService/StopJob", true},
		{"admin can get status", []string{"admin"}, "/jobctl.JobService/GetStatus", true},
		{"admin can stream output", []string{"admin"}, "/jobctl.JobService/StreamOutput", true},

		{"user cannot start job", []string{"user"}, "/jobctl.JobService/StartJob", false},
		{"user cannot stop job", []string{"user"}, "/jobctl.JobService/StopJob", false},
		{"user can get status", []string{"user"}, "/jobctl.JobService/GetStatus", true},
		{"user can stream output", []string{"user"}, "/jobctl.JobService/StreamOutput", true},

		{"unknown user cannot start job", []string{"unknown"}, "/jobctl.JobService/StartJob", false},
		{"unknown user cannot stop job", []string{"unknown"}, "/jobctl.JobService/StopJob", false},
		{"unknown user cannot get status", []string{"unknown"}, "/jobctl.JobService/GetStatus", false},
		{"unknown user cannot stream output", []string{"unknown"}, "/jobctl.JobService/StreamOutput", false},
		// edge cases
		{"empty sans not authorized", []string{}, "/jobctl.JobService/StartJob", false},
		{"nil sans not authorized", nil, "/jobctl.JobService/StartJob", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			san := ""
			if len(test.sans) > 0 {
				san = test.sans[0]
			}
			got := isAuthorized(san, test.method)
			if got != test.wantAuth {
				t.Errorf("isAuthorized(%q, %q) = %v, want %v", san, test.method, got, test.wantAuth)
			}
		})
	}
}

// func TestAuthWithClients(t *testing.T) {
// 	tests := []struct {
// 		name      string
// 		user      string     // The user identity (SAN) being tested
// 		operation string     // The RPC being called
// 		wantCode  codes.Code // Expected gRPC status
// 	}{
// 		// Admin: Full Access
// 		{"admin can start job", "admin", "StartJob", codes.OK},
// 		{"admin can stop job", "admin", "StopJob", codes.OK},
// 		{"admin can get status", "admin", "GetStatus", codes.OK},
// 		{"admin can stream output", "admin", "StreamOutput", codes.OK},

// 		// User: Read-only Access
// 		{"user cannot start job", "user", "StartJob", codes.PermissionDenied},
// 		{"user cannot stop job", "user", "StopJob", codes.PermissionDenied},
// 		{"user can get status", "user", "GetStatus", codes.OK},
// 		{"user can stream output", "user", "StreamOutput", codes.OK},

// 		// Unknown: No Access
// 		{"unknown cannot start", "unknown", "StartJob", codes.Unauthenticated},
// 		{"unknown cannot status", "unknown", "GetStatus", codes.Unauthenticated},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			// We use 'admin' as the creator to ensure a job exists
// 			clients, cleanup := createNewServerMultipleClients(t, "admin", tt.user)
// 			defer cleanup()

// 			adminClient := clients[0]
// 			testClient := clients[1]

// 			// Create a job using the admin client
// 			startResp, err := adminClient.StartJob(context.Background(), &pb.StartJobRequest{
// 				Command: []string{"echo", "hello"},
// 			})
// 			if err != nil {
// 				t.Fatalf("setup: admin failed to start job: %v", err)
// 			}

// 			// Perform the tested operation with the test client
// 			var opErr error
// 			ctx := context.Background()

// 			switch tt.operation {
// 			case "StartJob":
// 				_, opErr = testClient.StartJob(ctx, &pb.StartJobRequest{Command: []string{"ls"}})
// 			case "StopJob":
// 				_, opErr = testClient.StopJob(ctx, &pb.StopJobRequest{JobId: startResp.JobId})
// 			case "GetStatus":
// 				_, opErr = testClient.GetStatus(ctx, &pb.GetStatusRequest{JobId: startResp.JobId})
// 			case "StreamOutput":
// 				stream, err := testClient.StreamOutput(ctx, &pb.StreamOutputRequest{JobId: startResp.JobId})
// 				if err == nil {
// 					_, opErr = stream.Recv() // Recv triggers the interceptor logic
// 				} else {
// 					opErr = err
// 				}
// 			}

// 			// Verify the gRPC status code
// 			st, _ := status.FromError(opErr)
// 			if st.Code() != tt.wantCode {
// 				t.Errorf("operation %s for %s: got %v, want %v (msg: %q)",
// 					tt.operation, tt.user, st.Code(), tt.wantCode, st.Message())
// 			}
// 		})
// 	}
// }

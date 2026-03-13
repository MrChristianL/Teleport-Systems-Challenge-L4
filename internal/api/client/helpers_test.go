/* client_test.go

Client_test houses helper functions to assist in testing for other client operations
*/

package client

import (
	"net"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/api/server"
	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/worker"
	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"

	"google.golang.org/grpc"
)

func certsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "../../../certs")
}

func startTestServer(t *testing.T) (string, func()) {
	t.Helper()

	certs := certsDir()
	creds, err := server.ConfigureServerTLS(
		filepath.Join(certs, "ca-cert.pem"),
		filepath.Join(certs, "server-cert.pem"),
		filepath.Join(certs, "server-key.pem"),
	)
	if err != nil {
		t.Fatalf("failed to configure client TLS: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.Creds(creds),
		grpc.UnaryInterceptor(server.UnaryInterceptor),
		grpc.StreamInterceptor(server.StreamInterceptor),
	)
	tracker := worker.NewTracker()
	pb.RegisterJobServiceServer(s, server.NewServerWithTracker(tracker))
	go s.Serve(lis)

	return lis.Addr().String(), func() { s.Stop() }
}

func newTestClient(t *testing.T, addr string, certPrefix string) (*Client, func()) {
	t.Helper()

	certs := certsDir()
	client, err := NewClient(
		addr,
		filepath.Join(certs, certPrefix+"-cert.pem"),
		filepath.Join(certs, certPrefix+"-key.pem"),
		filepath.Join(certs, "ca-cert.pem"),
	)
	if err != nil {
		t.Fatalf("failed to create client as %s: %v", certPrefix, err)
	}

	return client, func() { client.Close() }
}

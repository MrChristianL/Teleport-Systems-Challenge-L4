package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"
	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/worker"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestTLSVersion verifies TLS 1.3 is enforced by the server
func TestTLSVersion(t *testing.T) {
	certs := certsDir()

	// Start a test server
	serverCreds, err := ConfigureServerTLS(
		filepath.Join(certs, "ca-cert.pem"),
		filepath.Join(certs, "server-cert.pem"),
		filepath.Join(certs, "server-key.pem"),
	)
	if err != nil {
		t.Fatalf("failed to configure server TLS: %v", err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.Creds(serverCreds),
		grpc.UnaryInterceptor(UnaryInterceptor),
		grpc.StreamInterceptor(StreamInterceptor),
	)

	tracker := worker.NewTracker()
	pb.RegisterJobServiceServer(s, &server{tracker: tracker})
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	// Build a TLS 1.2-only client config
	clientCert, err := tls.LoadX509KeyPair(
		filepath.Join(certs, "admin-cert.pem"),
		filepath.Join(certs, "admin-key.pem"),
	)
	if err != nil {
		t.Fatalf("failed to load client cert: %v", err)
	}

	caCert, err := os.ReadFile(filepath.Join(certs, "ca-cert.pem"))
	if err != nil {
		t.Fatalf("failed to load CA cert: %v", err)
	}

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(caCert)

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12, // Force TLS 1.2 — server should reject this
	})

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer conn.Close()

	// The TLS handshake happens on the first RPC, not at dial time
	client := pb.NewJobServiceClient(conn)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	_, err = client.GetStatus(ctx, &pb.GetStatusRequest{JobId: "test"})
	if err == nil {
		t.Error("expected server to reject TLS 1.2 connection, but RPC succeeded")
	}
}

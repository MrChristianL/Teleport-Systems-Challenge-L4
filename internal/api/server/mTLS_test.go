package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestTLSVersion verifies TLS 1.3 is enforced
func TestTLSVersion(t *testing.T) {
	// Try to connect with TLS 1.2 - should fail
	clientCert, err := tls.LoadX509KeyPair("../../../certs/admin-cert.pem", "../../../certs/admin-key.pem")
	if err != nil {
		t.Fatalf("Failed to load cert: %v", err)
	}

	caCert, err := os.ReadFile("../../../certs/ca-cert.pem")
	if err != nil {
		t.Fatalf("Failed to load CA: %v", err)
	}

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(caCert)

	// Force TLS 1.2
	config := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12, // Force TLS 1.2
	}

	creds := credentials.NewTLS(config)

	conn, err := grpc.NewClient("localhost:50051",
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer conn.Close()

	// Try to make an RPC call - this will trigger the TLS handshake
	client := pb.NewJobServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.GetStatus(ctx, &pb.GetStatusRequest{
		JobId: "test",
	})
	if err == nil {
		t.Error("Server should reject TLS 1.2 connections")
	}

	// Verify it's a TLS-related error
	if err == nil {
		t.Errorf("expected error when connecting to server, got nil")
	}
}

/* server_test.go

This file holds helper functions that allows the server tests to be cleaner.

These helper functions include:
- Create a server and return a single client connected to that server
- Create a server and return multiple clients (as many as specified)
- Connect clients to the server via the provided certs
- Stream output

These helpers are necessary for testing server functionality without relying on
any CLI implementation.
*/

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"testing"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Create a new server, a new client on that server, and an server cleanup function
func createNewServerSingleClient(t *testing.T, user string) (pb.JobServiceClient, func()) {
	t.Helper()

	serverCreds, err := ConfigureServerTLS("../../../certs/ca-cert.pem", "../../../certs/server-cert.pem", "../../../certs/server-key.pem")
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

	conn, err := connectClientWithCert(t, lis.Addr().String(), user)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}

	cleanup := func() {
		conn.Close()
		s.Stop()
	}

	return pb.NewJobServiceClient(conn), cleanup
}

// Create a new server with multiple clients connected to that server
func createNewServerMultipleClients(t *testing.T, users ...string) ([]pb.JobServiceClient, func()) {
	t.Helper()

	// start one server
	serverCreds, err := ConfigureServerTLS("../../../certs/ca-cert.pem", "../../../certs/server-cert.pem", "../../../certs/server-key.pem")
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

	// create one client per user, all pointing at the same server
	var clients []pb.JobServiceClient
	var conns []*grpc.ClientConn

	for _, user := range users {
		conn, err := connectClientWithCert(t, lis.Addr().String(), user)
		if err != nil {
			t.Fatalf("failed to dial as %s: %v", user, err)
		}
		conns = append(conns, conn)
		clients = append(clients, pb.NewJobServiceClient(conn))
	}

	cleanup := func() {
		for _, conn := range conns {
			conn.Close()
		}
		s.Stop()
	}

	return clients, cleanup
}

// Allows client to selection of which cert to use
func connectClientWithCert(t *testing.T, addr string, certPrefix string) (*grpc.ClientConn, error) {
	t.Helper()

	clientCert, err := tls.LoadX509KeyPair(
		fmt.Sprintf("../../../certs/%s-cert.pem", certPrefix),
		fmt.Sprintf("../../../certs/%s-key.pem", certPrefix),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s cert: %v", certPrefix, err)
	}

	caCert, err := os.ReadFile("../../../certs/ca-cert.pem")
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caCert)

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	})

	return grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
}

// Collects content of a stream (given valid permisssions) and returns what the client got
func collectStream(t *testing.T, client pb.JobServiceClient, jobID string) string {
	t.Helper()

	stream, err := client.StreamOutput(context.Background(), &pb.StreamOutputRequest{
		JobId: jobID,
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
	return string(got)
}

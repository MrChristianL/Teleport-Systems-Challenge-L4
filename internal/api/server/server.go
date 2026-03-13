package server

import (
	"fmt"
	"net"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/worker"
	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"
	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedJobServiceServer
	tracker *worker.Tracker
}

func NewServer() error {
	// load TLS config
	tlsCreds, err := ConfigureServerTLS(
		"certs/ca-cert.pem",
		"certs/server-cert.pem",
		"certs/server-key.pem",
	)
	if err != nil {
		return fmt.Errorf("loading TLS configuration: %w", err)
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		return fmt.Errorf("listening on port 50051: %w", err)
	}

	s := grpc.NewServer(
		grpc.Creds(tlsCreds),
		grpc.UnaryInterceptor(UnaryInterceptor),
		grpc.StreamInterceptor(StreamInterceptor),
	)

	// create a job tracker
	tracker := worker.NewTracker()

	pb.RegisterJobServiceServer(s, &server{tracker: tracker})

	if err = s.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}
	return nil
}

// this is a helper function to be utilized for client testing
func NewServerWithTracker(tracker *worker.Tracker) *server {
	return &server{tracker: tracker}
}

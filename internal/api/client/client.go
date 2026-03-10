package client

import (
	"fmt"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"

	"google.golang.org/grpc"
)

type Client struct {
	conn   *grpc.ClientConn
	client pb.JobServiceClient
}

// NewClient creates a new client connection to the job service with mTLS
func NewClient(serverAddr string, certFile string, keyFile string, caFile string) (*Client, error) {
	// Configure client TLS credentials
	creds, err := ConfigureClientTLS(certFile, keyFile, caFile)
	if err != nil {
		return nil, fmt.Errorf("configuring client TLS: %v", err)
	}

	// Create gRPC connection with mTLS
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("connecting to server: %v", err)
	}

	return &Client{
		conn:   conn,
		client: pb.NewJobServiceClient(conn),
	}, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// TODO: For production, add ServerName as a parameter for creating a new client
// to support connections where the target address differs from the cert's DNS SAN

package client

import (
	"context"
	"fmt"
	"io"

	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"
)

func (c *Client) StartJob(ctx context.Context, command []string) (jobID string, err error) {
	resp, err := c.client.StartJob(ctx, &pb.StartJobRequest{
		Command: command,
	})
	if err != nil {
		return "", err
	}
	return resp.JobId, nil
}

func (c *Client) StopJob(ctx context.Context, jobID string) (success bool, message string, err error) {
	resp, err := c.client.StopJob(ctx, &pb.StopJobRequest{
		JobId: jobID,
	})
	if err != nil {
		return false, "", fmt.Errorf("failed to stop job: %v", err)
	}

	return resp.Success, resp.Message, nil
}

func (c *Client) GetStatus(ctx context.Context, jobID string) (status pb.GetStatusResponse_Status, exitCode int32, duration string, err error) {
	resp, err := c.client.GetStatus(ctx, &pb.GetStatusRequest{
		JobId: jobID,
	})
	if err != nil {
		return pb.GetStatusResponse_UNKNOWN, -1, "", fmt.Errorf("failed to get status: %v", err)
	}

	return resp.Status, resp.ExitCode, resp.Message, nil
}

// StreamOutput streams the output of a job, calling the handler for each chunk of output
// The function returns when the stream ends (io.EOF) or the handler returns an error
func (c *Client) StreamOutput(ctx context.Context, jobID string, handler func(chunk []byte) error) error {
	stream, err := c.client.StreamOutput(ctx, &pb.StreamOutputRequest{
		JobId: jobID,
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %v", err)
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			// stream ended normally
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream error: %v", err)
		}

		// call handler with chunk
		if err := handler(resp.Chunk); err != nil {
			return fmt.Errorf("handler returned error: %v", err)
		}
	}
}

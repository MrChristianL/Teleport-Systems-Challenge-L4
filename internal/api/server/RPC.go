package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/worker"
	pb "github.com/mrchristianl/teleport-systems-challenge-l4/protobuf/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *server) StartJob(ctx context.Context, req *pb.StartJobRequest) (*pb.StartJobResponse, error) {

	// create job, add to job tracker
	job, err := s.tracker.AddJob(req.Command)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create job")
	}

	if err := job.Start(); err != nil {
		return nil, status.Errorf(codes.Internal, "%s", userFriendlyStartError(err))
	}

	return &pb.StartJobResponse{
		JobId: job.ID,
	}, nil
}

func (s *server) StopJob(ctx context.Context, req *pb.StopJobRequest) (*pb.StopJobResponse, error) {
	// retrieve job from tracker
	job, err := s.tracker.GetJob(req.JobId)
	if err != nil {
		return &pb.StopJobResponse{
			Success: false,
			Message: fmt.Sprintf("[%s]: %v", req.JobId, err),
		}, nil
	}

	// stop job
	if err := job.Stop(); err != nil {
		return &pb.StopJobResponse{
			Success: false,
			Message: fmt.Sprintf("[%s] failed to stop job: %v", req.JobId, err),
		}, nil
	}

	return &pb.StopJobResponse{
		Success: true,
		Message: "Success: job stopped",
	}, nil
}

func (s *server) GetStatus(ctx context.Context, req *pb.GetStatusRequest) (*pb.GetStatusResponse, error) {
	job, err := s.tracker.GetJob(req.JobId)
	if err != nil {
		return &pb.GetStatusResponse{
			Status:   pb.GetStatusResponse_UNKNOWN,
			ExitCode: -1,
			Message:  fmt.Sprintf("[%s]: %v", req.JobId, err),
		}, nil
	}

	// map worker.Status to protoBuf Status
	status := job.Status()
	var pbStatus pb.GetStatusResponse_Status
	switch status {
	case worker.Running:
		pbStatus = pb.GetStatusResponse_RUNNING
	case worker.Finished:
		pbStatus = pb.GetStatusResponse_FINISHED
	case worker.Failed:
		pbStatus = pb.GetStatusResponse_FAILED
	case worker.Stopped:
		pbStatus = pb.GetStatusResponse_STOPPED
	default:
		pbStatus = pb.GetStatusResponse_UNKNOWN
	}

	return &pb.GetStatusResponse{
		Status:   pbStatus,
		ExitCode: int32(job.ExitCode()),
		Message:  job.StopReason(),
	}, nil
}

func (s *server) StreamOutput(req *pb.StreamOutputRequest, stream pb.JobService_StreamOutputServer) error {
	job, err := s.tracker.GetJob(req.JobId)
	if err != nil {
		return status.Errorf(codes.NotFound, "[%s] job not found: %v", req.JobId, err)
	}

	clientID := fmt.Sprintf("grpc-stream-%s-%d", req.JobId, time.Now().UnixNano())

	var output bytes.Buffer
	if err := job.StreamFromDisk(stream.Context(), &output); err != nil {
		if stream.Context().Err() != nil {
			return status.Errorf(codes.Canceled, "[%s] stream canceled", clientID)
		}
		return status.Errorf(codes.Internal, "[%s] stream failed: %v", clientID, err)
	}
	return nil
}

func userFriendlyStartError(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		switch {
		case errors.Is(pathErr.Err, fs.ErrNotExist):
			return fmt.Sprintf("executable not found at %q", pathErr.Path)
		case errors.Is(pathErr.Err, fs.ErrPermission):
			return fmt.Sprintf("permission denied: %q is not executable", pathErr.Path)
		case errors.Is(pathErr.Err, syscall.ENOEXEC):
			return fmt.Sprintf("file is not a valid executable: %q", pathErr.Path)
		}
	}

	return "failed to start job"
}

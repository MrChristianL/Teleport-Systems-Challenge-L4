package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

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
	snapshot := job.Snapshot()

	// worker.Created is intentionally left off of this switch. Clients never receive a job ID
	// before Start() transitions the status to Running, so Created is unobservable via this GetStatus call.
	// If a ListJobs action or a two-step create/run flow were added to the CLI in the future,
	// this would need to be reflected in both the proto and this switch.
	var pbStatus pb.GetStatusResponse_Status
	switch snapshot.Status {
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
		ExitCode: int32(snapshot.ExitCode),
		Message:  snapshot.StopReason,
	}, nil
}

func (s *server) StreamOutput(req *pb.StreamOutputRequest, stream pb.JobService_StreamOutputServer) error {
	job, err := s.tracker.GetJob(req.JobId)
	if err != nil {
		return status.Errorf(codes.NotFound, "[%s] job not found: %v", req.JobId, err)
	}

	w := &streamWriter{stream: stream}
	if err := job.StreamFromDisk(stream.Context(), w); err != nil {
		if stream.Context().Err() != nil {
			return status.Errorf(codes.Canceled, "stream canceled")
		}
		return status.Errorf(codes.Internal, "stream failed: %v", err)
	}
	return nil
}

type streamWriter struct {
	stream pb.JobService_StreamOutputServer
}

func (w *streamWriter) Write(p []byte) (int, error) {
	chunk := make([]byte, len(p))
	copy(chunk, p)
	if err := w.stream.Send(&pb.StreamOutputResponse{Chunk: chunk}); err != nil {
		return 0, err
	}
	return len(p), nil
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

	// default message
	return "failed to start job"
}

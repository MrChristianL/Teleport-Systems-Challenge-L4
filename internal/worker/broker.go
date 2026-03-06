/* broker.go

Broker handles the writing and file handling for output created by jobs,
as well as the coordination of *sync.Cond and Mutex for thread safe multiclient output streaming
*/

package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type broker struct {
	mu   sync.Mutex
	cond *sync.Cond // used for output streaming
	done chan struct{}

	file         *os.File
	filePath     string
	totalWritten int64 // bytes commited to disk
	closed       bool
}

func newBroker(jobID string) (*broker, error) {
	filePath := filepath.Join(os.TempDir(), fmt.Sprintf("jobctl-%s.log", jobID))
	file, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}

	b := &broker{
		file:     file,
		filePath: filePath,
		done:     make(chan struct{}),
	}

	b.cond = sync.NewCond(&b.mu)
	return b, nil
}

// Write implements io.Writer. It streams stdout/stderr output to a logfile and
// signals readers once the data is visible in the kernel buffer
func (b *broker) Write(data []byte) (bytesWritten int, err error) {
	if len(data) == 0 {
		return 0, nil
	}

	// Hold the lock to ensure the atomicity of the write operation
	// relative to the totalWritten counter and the Broadcast signal
	b.mu.Lock()
	defer b.mu.Unlock()

	// If the broker is closed, it cannot be written to
	if b.closed {
		return 0, io.ErrClosedPipe
	}

	// Commit data to log, store bytes written.
	// POSIX guarantees data written will be available to readers via the kernel page cache.
	bytesWritten, err = b.file.Write(data)
	if err != nil {
		return bytesWritten, fmt.Errorf("failed to write to log file: %w", err)
	}

	// Increment totalWritten only after the write succeeds
	b.totalWritten += int64(bytesWritten)

	// Wake up any readers in Wait()
	b.cond.Broadcast()
	return bytesWritten, nil
}

func (b *broker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// already closed
	if b.closed {
		return
	}

	b.closed = true
	b.file.Close()
	close(b.done)

	// When the broker is closed, readers are woken up one last time
	// to read what data they may have left to take in, then exit
	b.cond.Broadcast()
}

// streamFromDisk tails the log file from the beginning and writes to 'out'.
// It blocks and waits for new data until the context is canceled or the broker is closed.
func (b *broker) streamFromDisk(ctx context.Context, out io.Writer) error {
	f, err := os.Open(b.filePath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Watchdog to ensure readers do not indefinitely hang on cancelation
	go func() {
		select {
		case <-ctx.Done():
		case <-b.done: // Exit if the broker closes naturally
			return
		}
		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	}()

	buf := make([]byte, chunkReadSize) // 32 KiB read buffer
	var offset int64

	for {
		// Check context at the start of each loop iteration.
		if err := ctx.Err(); err != nil {
			return err
		}

		bytesRead, err := f.Read(buf)
		if bytesRead > 0 {
			if _, werr := out.Write(buf[:bytesRead]); werr != nil {
				return werr
			}
			offset += int64(bytesRead)
		}

		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("failed to read log file: %w", err)
			}

			b.mu.Lock()

			// Wait loop:
			// if broker is not closed, but we have read all available content
			for !b.closed && b.totalWritten == offset {
				if ctx.Err() != nil {
					b.mu.Unlock()
					return ctx.Err()
				}
				b.cond.Wait()
			}

			isClosed := b.closed
			finalWritten := b.totalWritten
			b.mu.Unlock()

			// if broker is closed and all data has been read, exit
			if isClosed && finalWritten == offset {
				return nil
			}

			// Otherwise, more data was written while we were locking/waiting; loop back.
			continue
		}
	}
}

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

const chunkReadSize = 32 * 1024 // 32 KiB

type broker struct {
	mu   sync.Mutex
	cond *sync.Cond // used to broadcast new writes to open readers
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
		return nil, fmt.Errorf("creating output file: %w", err)
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
		return bytesWritten, fmt.Errorf("writing to log file: %w", err)
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

	// if broker is already closed
	if b.closed {
		return
	}

	b.closed = true
	b.file.Close() //closes the log file
	close(b.done)  // b.done signals to readers that no more output will be committed

	// When the broker is closed, readers are woken up one last time
	// to read what data they may have left to take in, then exit
	b.cond.Broadcast()
}

// streamFromDisk tails the log file from the beginning and writes to 'out'.
// It blocks and waits for new data until the context is canceled or the broker is closed.
func (b *broker) streamFromDisk(ctx context.Context, out io.Writer) error {
	f, err := os.Open(b.filePath)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
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

	// buf := make([]byte, chunkReadSize) // 32 KiB read buffer
	var offset int64

	for {
		// Check context at the start of each loop iteration.
		if err := ctx.Err(); err != nil {
			return err
		}

		bytesRead, err := io.Copy(out, f)
		offset += bytesRead

		if err != nil {
			return fmt.Errorf("reading log file: %w", err)
		}

		// io.Copy never returns io.EOF
		// To enter wait loop, readers much check for no progress (bytesRead == 0) with (err == nil)
		if bytesRead == 0 {
			b.mu.Lock()

			// if broker is not closed, but we have read all avialable content, reader waits
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
		}
	}
}

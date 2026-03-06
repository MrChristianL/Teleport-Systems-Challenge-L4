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

// Writes stdout/stderr pipes to log file and wakes readers when new content is committed to disk. Tracks bytes written for reader offset tracking
func (b *broker) Write(data []byte) (bytesWritten int, err error) {
	if len(data) == 0 {
		return 0, nil
	}

	// hold lock during Write+Sync to ensure atomic: write → sync → update → broadcast
	// this guarantees readers never see a totalWritten value for un-synced data
	b.mu.Lock()
	defer b.mu.Unlock()

	// if the broker is closed, it cannot be written to
	if b.closed {
		return 0, nil
	}

	// commit data to log, store bytes written
	bytesWritten, err = b.file.Write(data)
	if err != nil {
		return bytesWritten, fmt.Errorf("failed to write to log file: %w", err)
	}

	// sync log file to ensure write to disk - readers never see uncommitted data
	if err := b.file.Sync(); err != nil {
		return bytesWritten, fmt.Errorf("failed to sync log file: %w", err)
	}

	// incremented totalWritten after sync to ensure readers
	// new data is available only after the data is committed to disk
	b.totalWritten += int64(bytesWritten)
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

	// when the broker is closed, readers are woken up one last time
	// to read what data they may have left to take in, then exit
	b.cond.Broadcast()
}

func (b *broker) streamFromDisk(ctx context.Context, handler func([]byte) error) error {
	f, err := os.Open(b.filePath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	defer f.Close()

	// watchdog to ensure readers do not indefinitely hang on cancelation
	go func() {
		select {
		case <-ctx.Done(): // user disconnects
		case <-b.done: // server-side completion (e.g. broker closed, job finished)
			return
		}

		b.mu.Lock()
		b.cond.Broadcast()
		b.mu.Unlock()
	}()

	buf := make([]byte, chunkReadSize) // 32 KiB read buffer
	var offset int64

	for {
		bytesRead, err := f.Read(buf)
		if bytesRead > 0 {
			offset += int64(bytesRead)
			if herr := handler(buf[:bytesRead]); herr != nil {
				return herr
			}
		}

		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("failed to read log file: %w", err)
			}

			// caught up to write head, reader waits
			b.mu.Lock()

			// read all currently available data, but broker isn't closed - wait
			for !b.closed && b.totalWritten == offset {
				if ctx.Err() != nil {
					b.mu.Unlock()
					return ctx.Err()
				}
				b.cond.Wait()
			}
			closed := b.closed
			b.mu.Unlock()

			// read all data and broker closed, so no new data to be read - exit
			if closed && b.totalWritten == offset {
				return nil
			}

			// loop back and read again
			continue
		}
	}
}

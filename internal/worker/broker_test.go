package worker

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestLargeChunks verifies reader can receive data even if data is larger than chunkReadSize
func TestLargeChunks(t *testing.T) {
	broker, err := newBroker("test-job-4")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	largeData := make([]byte, chunkReadSize*2)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	broker.Write(largeData)
	broker.close()

	var got []byte
	broker.streamFromDisk(context.Background(), func(chunk []byte) error {
		got = append(got, chunk...)
		return nil
	})

	if len(got) != len(largeData) {
		t.Errorf("got %d bytes, want %d", len(got), len(largeData))
	}
	for i := range largeData {
		if i >= len(got) || got[i] != largeData[i] {
			t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], largeData[i])
			break
		}
	}
}

// TestBinaryData verifies binary data remain intact when read from log file
func TestBinaryData(t *testing.T) {
	broker, err := newBroker("test-job-5")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	binaryData := []byte{
		0x00, 0xFF, 0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A,
		0x00, 0x01, 0x02, 0x03, 0x0A, 0x0D,
		0xDE, 0xAD, 0xBE, 0xEF,
	}

	broker.Write(binaryData)
	broker.close()

	var got []byte
	broker.streamFromDisk(context.Background(), func(chunk []byte) error {
		got = append(got, chunk...)
		return nil
	})

	if len(got) != len(binaryData) {
		t.Errorf("got %d bytes, want %d", len(got), len(binaryData))
	}
	for i, b := range binaryData {
		if i >= len(got) || got[i] != b {
			t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], b)
		}
	}
}

// TestClosedBroker verifies Writes to a closed broker do not get appended to log file
func TestClosedBroker(t *testing.T) {
	broker, err := newBroker("test-job-6")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	broker.Write([]byte("open"))
	broker.close()

	// Write after close — should be ignored
	broker.Write([]byte("closed"))

	// two late subscribers — both should get complete history only
	collect := func() chan []byte {
		ch := make(chan []byte, 1)
		go func() {
			var got []byte
			broker.streamFromDisk(context.Background(), func(chunk []byte) error {
				got = append(got, chunk...)
				return nil
			})
			ch <- got
		}()
		return ch
	}

	ch1 := collect()
	ch2 := collect()

	want := "open"
	if got := <-ch1; string(got) != want {
		t.Errorf("client-1: got %q, want %q", string(got), want)
	}
	if got := <-ch2; string(got) != want {
		t.Errorf("client-2: got %q, want %q", string(got), want)
	}
}

// TestBrokerConcurrentStreaming verifies sync.Cond broadcasts wake all waiting clients
func TestBrokerConcurrentStreaming(t *testing.T) {
	broker, err := newBroker("test-job-7")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	const numClients = 10
	const numWrites = 5

	var wg sync.WaitGroup
	var readyWg sync.WaitGroup
	wg.Add(numClients)
	readyWg.Add(numClients)

	// Start 10 concurrent clients
	results := make([][]byte, numClients)
	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			defer wg.Done()
			readyWg.Done() // Signal ready

			broker.streamFromDisk(context.Background(), func(chunk []byte) error {
				results[clientID] = append(results[clientID], chunk...)
				return nil
			})
		}(i)
	}

	// Wait for all clients to be ready
	readyWg.Wait()

	// Write data - should wake all clients via Broadcast
	for i := 0; i < numWrites; i++ {
		broker.Write([]byte(fmt.Sprintf("chunk-%d", i)))
	}

	broker.close()
	wg.Wait()

	// Verify all clients got same data
	expected := string(results[0])
	for i := 1; i < numClients; i++ {
		if string(results[i]) != expected {
			t.Errorf("client %d got different data than client 0", i)
		}
	}
}

// TestNoLostWakeup verifies offset check prevents lost broadcast signals
func TestNoLostWakeup(t *testing.T) {
	broker, err := newBroker("test-job-race")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	// Write initial data
	broker.Write([]byte("initial"))

	// Start reader that will hit EOF
	readerStarted := make(chan struct{})
	readerDone := make(chan []byte, 1)

	go func() {
		var got []byte
		close(readerStarted)
		broker.streamFromDisk(context.Background(), func(chunk []byte) error {
			got = append(got, chunk...)
			return nil
		})
		readerDone <- got
	}()

	// Wait for reader to start
	<-readerStarted

	// small delay to let reader hit EOF (required for this race test)
	time.Sleep(10 * time.Millisecond)

	// Write more data - this tests the race window
	broker.Write([]byte("racing"))
	broker.Write([]byte("data"))

	broker.close()

	got := <-readerDone

	if !bytes.Contains(got, []byte("initial")) ||
		!bytes.Contains(got, []byte("racing")) ||
		!bytes.Contains(got, []byte("data")) {
		t.Errorf("got %q, missing some writes (potential lost wakeup)", got)
	}
}

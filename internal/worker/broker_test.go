package worker

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"
	"testing"
)

// TestLargeChunks verifies reader can receive data even if data is larger than chunkReadSize
func TestLargeChunks(t *testing.T) {
	broker, err := newBroker("test-job-4")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	broker.Write(largeData)
	broker.close()

	var buf bytes.Buffer
	if err := broker.streamFromDisk(t.Context(), &buf); err != nil {
		log.Printf("streamFromDisk Error: %v", err)
	}

	if buf.Len() != len(largeData) {
		t.Errorf("got %d bytes, want %d", buf.Len(), len(largeData))
	}
	if !bytes.Equal(buf.Bytes(), largeData) {
		t.Errorf("content mismatch")
	}
}

// TestClosedBroker verifies Writes after close are handled correctly and that multiple clients can
// stream the same historic data from a closed broker
func TestClosedBroker(t *testing.T) {
	broker, err := newBroker("test-job-6")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	broker.Write([]byte("open"))
	broker.close()

	_, err = broker.Write([]byte("closed"))
	if err == nil {
		t.Fatalf("expected error writing to closed broker, got none")
	}
	if err != io.ErrClosedPipe {
		t.Errorf("expected io.ErrClosedPipe, got %v", err)
	}

	want := []byte("open")

	var buf1 bytes.Buffer
	if err := broker.streamFromDisk(t.Context(), &buf1); err != nil {
		t.Fatalf("client-1 stream error: %v", err)
	}
	if !bytes.Equal(buf1.Bytes(), want) {
		t.Errorf("client-1: got %q, want %q", buf1.Bytes(), want)
	}

	var buf2 bytes.Buffer
	if err := broker.streamFromDisk(t.Context(), &buf2); err != nil {
		t.Fatalf("client-2 stream error: %v", err)
	}
	if !bytes.Equal(buf2.Bytes(), want) {
		t.Errorf("client-2: got %q, want %q", buf2.Bytes(), want)
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

	outputs := make([]bytes.Buffer, numClients)

	for i := range numClients {
		go func(clientID int) {
			defer wg.Done()

			// Signals that the reader is entering the wait loop
			readyWg.Done()
			if err := broker.streamFromDisk(t.Context(), &outputs[clientID]); err != nil {
				t.Logf("streamFromDisk Error: %v", err)
			}
		}(i)
	}

	readyWg.Wait()

	for i := range numWrites {
		fmt.Fprintf(broker, "chunk-%d", i)
	}

	broker.close()

	wg.Wait()

	expected := []byte("chunk-0chunk-1chunk-2chunk-3chunk-4")
	for i := range numClients {
		if !bytes.Equal(outputs[i].Bytes(), expected) {
			t.Errorf("client %d got different data", i)
		}
	}
}

// TestBrokerStreamCompleteness verifies that a reader receives all writes
// including those that arrive after the initial EOF, before broker close.
func TestBrokerStreamCompleteness(t *testing.T) {
	broker, err := newBroker("test-job-8")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	broker.Write([]byte("initial"))

	var wg sync.WaitGroup
	var readyWg sync.WaitGroup

	readyWg.Add(1)

	readerOutput := make(chan []byte, 1)

	wg.Go(func() {
		var buf bytes.Buffer
		readyWg.Done()
		if err := broker.streamFromDisk(t.Context(), &buf); err != nil {
			log.Printf("streamFromDisk Error: %v", err)
		}
		readerOutput <- buf.Bytes()
	})
	readyWg.Wait()

	broker.Write([]byte("racing"))
	broker.Write([]byte("data"))
	broker.close()

	got := <-readerOutput

	for _, want := range []string{"initial", "racing", "data"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("got %q, missing %q (potential lost wakeup)", string(got), want)
		}
	}
}

package worker

import (
	"bytes"
	"fmt"
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

	largeData := make([]byte, chunkReadSize*2)
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

	// attempt write to closed broker
	broker.Write([]byte("closed"))

	collect := func() chan []byte {
		ch := make(chan []byte, 1)
		go func() {
			var buf bytes.Buffer
			if err := broker.streamFromDisk(t.Context(), &buf); err != nil {
				log.Printf("streamFromDisk Error: %v", err)
			}
			ch <- buf.Bytes()
		}()
		return ch
	}

	ch1 := collect()
	ch2 := collect()

	want := []byte("open")
	if got := <-ch1; !bytes.Equal(got, want) {
		t.Errorf("client-1: got %q, want %q", string(got), string(want))
	}
	if got := <-ch2; !bytes.Equal(got, want) {
		t.Errorf("client-2: got %q, want %q", string(got), string(want))
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
				log.Printf("streamFromDisk Error: %v", err)
			}
		}(i)
	}

	readyWg.Wait()

	for i := range numWrites {
		broker.Write([]byte(fmt.Sprintf("chunk-%d", i)))
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

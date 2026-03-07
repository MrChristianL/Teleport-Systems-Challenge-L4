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

	var buf bytes.Buffer
	broker.streamFromDisk(context.Background(), &buf)

	if buf.Len() != len(largeData) {
		t.Errorf("got %d bytes, want %d", buf.Len(), len(largeData))
	}
	if !bytes.Equal(buf.Bytes(), largeData) {
		t.Errorf("content mismatch")
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

	var buf bytes.Buffer
	broker.streamFromDisk(context.Background(), &buf)

	if !bytes.Equal(buf.Bytes(), binaryData) {
		t.Errorf("got %x, want %x", buf.Bytes(), binaryData)
	}
}

// TestClosedBroker verifies Writes after close are handled correctly
func TestClosedBroker(t *testing.T) {
	broker, err := newBroker("test-job-6")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	broker.Write([]byte("open"))
	broker.close()

	_, err = broker.Write([]byte("closed"))

	collect := func() chan []byte {
		ch := make(chan []byte, 1)
		go func() {
			var buf bytes.Buffer
			broker.streamFromDisk(context.Background(), &buf)
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

	// Using a slice of buffers to capture output from each client
	outputs := make([]bytes.Buffer, numClients)

	for i := 0; i < numClients; i++ {
		go func(clientID int) {
			defer wg.Done()
			readyWg.Done()
			broker.streamFromDisk(context.Background(), &outputs[clientID])
		}(i)
	}

	readyWg.Wait()

	for i := 0; i < numWrites; i++ {
		broker.Write([]byte(fmt.Sprintf("chunk-%d", i)))
	}

	broker.close()
	wg.Wait()

	// Verify all clients got same data
	expected := outputs[0].Bytes()
	for i := 1; i < numClients; i++ {
		if !bytes.Equal(outputs[i].Bytes(), expected) {
			t.Errorf("client %d got different data", i)
		}
	}
}

// TestNoLostWakeup verifies offset check prevents lost broadcast signals
func TestNoLostWakeup(t *testing.T) {
	broker, err := newBroker("test-job-race")
	if err != nil {
		t.Fatalf("failed to create broker: %v", err)
	}

	broker.Write([]byte("initial"))

	readerStarted := make(chan struct{})
	readerDone := make(chan []byte, 1)

	go func() {
		var buf bytes.Buffer
		close(readerStarted)
		broker.streamFromDisk(context.Background(), &buf)
		readerDone <- buf.Bytes()
	}()

	<-readerStarted
	time.Sleep(10 * time.Millisecond)

	broker.Write([]byte("racing"))
	broker.Write([]byte("data"))
	broker.close()

	got := <-readerDone

	for _, want := range []string{"initial", "racing", "data"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("got %q, missing %q (potential lost wakeup)", string(got), want)
		}
	}
}

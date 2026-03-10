package main

import (
	"log"

	"github.com/mrchristianl/teleport-systems-challenge-l4/internal/api/server"
)

func main() {
	if err := server.NewServer(); err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
}

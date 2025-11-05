package main

import (
	"backend/internal/api"
	"backend/internal/engine"
	"log"
)

const (
	LOCAL_PATH  = "./local_data"
	REMOTE_PATH = "./remote_data"
	API_PORT    = "8080"
)

func main() {
	syncEngine, err := engine.NewSyncEngine(LOCAL_PATH, REMOTE_PATH)
	if err != nil {
		log.Fatalf("Failed to create sync engine: %v", err)
	}

	if err := syncEngine.Run(); err != nil {
		log.Fatalf("Failed to run sync engine: %v", err)
	}

	log.Println("Sync engine is running...")

	// Start API server
	apiServer := api.NewServer(syncEngine)
	if err := apiServer.Start(API_PORT); err != nil {
		log.Fatalf("Failed to start API server: %v", err)
	}
}

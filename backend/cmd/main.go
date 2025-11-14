package main

import (
	"backend/internal/api"
	"backend/internal/config"
	"backend/internal/engine"
	"backend/internal/storage"
	"log"
)

func main() {
	// Create a local storage provider
	localProvider, err := storage.NewFileSystemProvider(config.LOCAL_PATH)
	if err != nil {
		log.Fatalf("Failed to initialize local provider: %v", err)
	}

	// Create a remote storage provider
	remoteProvider, err := storage.NewFileSystemProvider(config.REMOTE_PATH)
	if err != nil {
		log.Fatalf("Failed to initialize remote provider: %v", err)
	}

	// Create sync engine instance
	syncEngine, err := engine.NewSyncEngine(localProvider, remoteProvider)
	if err != nil {
		log.Fatalf("Failed to create sync engine: %v", err)
	}

	// Run sync engine
	if err := syncEngine.Run(); err != nil {
		log.Fatalf("Failed to run sync engine: %v", err)
	}

	log.Println("Sync engine is running...")

	// Start API server
	apiServer := api.NewServer(syncEngine)
	if err := apiServer.Start(config.API_PORT); err != nil {
		log.Fatalf("Failed to start API server: %v", err)
	}
}

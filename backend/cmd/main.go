package main

import (
	"backend/internal/api"
	"backend/internal/engine"
	"backend/internal/storage"
	"backend/internal/utils"
	"log"
)

func main() {
	// Create a local storage provider
	localProvider, err := storage.NewFileSystemProvider(utils.LOCAL_PATH)
	if err != nil {
		log.Fatalf("Failed to initialize local provider: %v", err)
	}

	// Create a remote storage provider
	remoteProvider, err := storage.NewFileSystemProvider(utils.REMOTE_PATH)
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
	if err := apiServer.Start(utils.API_PORT); err != nil {
		log.Fatalf("Failed to start API server: %v", err)
	}
}

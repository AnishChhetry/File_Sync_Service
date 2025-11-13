package engine

import (
	"backend/internal/models"
	"backend/internal/storage"
	"fmt"
	"log"
	"os"

	"github.com/fsnotify/fsnotify"
)

// Processes a file system event.
func (s *SyncEngine) handleEvent(event fsnotify.Event) error {
	if s.IsPaused() {
		log.Printf("Sync paused, ignoring event for: %s\n", event.Name)
		return nil
	}

	isLocal, relPath, err := s.determineEventSource(event.Name)
	if err != nil {
		log.Printf("Error determining event source: %v\n", err)
		return nil
	}

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		return s.handleCreateEvent(event, isLocal, relPath)
	case event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename:
		return s.handleDeleteOrRenameEvent(event, isLocal, relPath)
	case event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Chmod == fsnotify.Chmod:
		return s.handleWriteOrChmodEvent(event, isLocal, relPath)
	}

	return nil
}

// Processes file/directory creation events.
func (s *SyncEngine) handleCreateEvent(event fsnotify.Event, isLocal bool, relPath string) error {
	info, err := os.Stat(event.Name)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", event.Name, err)
	}

	if info.IsDir() {
		log.Printf("Directory created: %s\n", event.Name)
		s.watcher.Add(event.Name)
		return s.syncDirectory(relPath, isLocal)
	}

	return s.syncFile(event, isLocal, relPath)
}

// Processes file deletion/rename events.
func (s *SyncEngine) handleDeleteOrRenameEvent(event fsnotify.Event, isLocal bool, relPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventType := "delete"
	message := fmt.Sprintf("File deleted: %s", relPath)
	if event.Op&fsnotify.Rename == fsnotify.Rename {
		eventType = "move"
		message = fmt.Sprintf("File moved or renamed: %s", relPath)
	}

	// Determine providers
	_, dstProvider := s.getProviders(isLocal)
	srcMap, dstMap := s.getStateMaps(isLocal)

	// Remove from state maps
	delete(*srcMap, relPath)
	delete(*dstMap, relPath)

	// Delete from destination
	if err := dstProvider.DeleteFile(relPath); err != nil && !os.IsNotExist(err) {
		log.Printf("error deleting file %s: %v\n", relPath, err)
	}

	// Notify callback
	if s.eventCallback != nil {
		direction := getDirection(isLocal)
		s.eventCallback(eventType, relPath, direction, message)
	}

	return nil
}

// Processes file modification events.
func (s *SyncEngine) handleWriteOrChmodEvent(event fsnotify.Event, isLocal bool, relPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Determine providers
	srcProvider, dstProvider := s.getProviders(isLocal)
	srcMap, dstMap := s.getStateMaps(isLocal)

	// Get source metadata
	srcMeta, err := srcProvider.GetMetadata(relPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("File %s no longer exists\n", event.Name)
			s.handleMissingFile(relPath, isLocal)
			return nil
		}
		return fmt.Errorf("error getting metadata for %s: %w", event.Name, err)
	}

	// Check if file exists in destination
	dstMeta, existsInDst := (*dstMap)[relPath]
	if existsInDst && srcMeta.Hash == dstMeta.Hash {
		// Update state maps and return
		(*srcMap)[relPath] = srcMeta
		(*dstMap)[relPath] = dstMeta
		return nil
	}

	// Handle file synchronization
	if !existsInDst || srcMeta.ModTime.After(dstMeta.ModTime) {
		return s.syncFileToDestination(srcProvider, dstProvider, srcMap, dstMap, relPath, srcMeta, isLocal)
	}

	// Handle conflict
	return s.handleFileConflict(relPath, srcMeta, dstMeta, isLocal)
}

// Copies file from source to destination provider.
func (s *SyncEngine) syncFileToDestination(src, dst storage.StorageProvider, srcMap, dstMap *map[string]models.FileMetadata, relPath string, meta models.FileMetadata, isLocal bool) error {
	direction := getDirection(isLocal)
	log.Printf("%s sync for %s\n", direction, relPath)

	if err := copyFile(src, dst, relPath, meta.ModTime); err != nil {
		return fmt.Errorf("error syncing file %s: %w", relPath, err)
	}

	// Update state maps
	(*srcMap)[relPath] = meta
	(*dstMap)[relPath] = meta

	// Notify callback
	if s.eventCallback != nil {
		s.eventCallback("sync", relPath, direction, fmt.Sprintf("File synced: %s", relPath))
	}

	return nil
}

// Logs and reports file conflicts.
func (s *SyncEngine) handleFileConflict(relPath string, srcMeta, dstMeta models.FileMetadata, isLocal bool) error {
	log.Printf("Conflict detected for file %s; destination file is newer\n", relPath)

	if s.eventCallback != nil {
		direction := getDirection(isLocal)
		s.eventCallback("conflict", relPath, direction, fmt.Sprintf("File conflict: %s (destination is newer)", relPath))
	}

	return nil
}

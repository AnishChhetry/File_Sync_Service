package engine

import (
	"backend/internal/models"
	"backend/internal/storage"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Handles synchronization between local and remote storage providers.
type SyncEngine struct {
	localProvider  storage.StorageProvider
	remoteProvider storage.StorageProvider

	localMap  map[string]models.FileMetadata
	remoteMap map[string]models.FileMetadata

	watcher       *fsnotify.Watcher
	mu            sync.RWMutex
	isPaused      bool
	pauseMu       sync.RWMutex
	eventCallback func(eventType, filePath, direction, message string)
}

// Represents information about a synchronized file.
type FileInfo struct {
	RelativePath string `json:"relativePath"`
	Hash         string `json:"hash"`
	ModTime      string `json:"modTime"`
	Location     string `json:"location"`
}

// Copies a file from src to dst storage providers.
func copyFile(src storage.StorageProvider, dst storage.StorageProvider, relativePath string, modTime time.Time) error {
	reader, err := src.GetReader(relativePath)
	if err != nil {
		return fmt.Errorf("failed to open source %s: %w", relativePath, err)
	}
	defer reader.Close()

	writer, err := dst.GetWriter(relativePath, modTime)
	if err != nil {
		return fmt.Errorf("failed to open destination %s: %w", relativePath, err)
	}

	if _, err := io.Copy(writer, reader); err != nil {
		writer.Close()
		return fmt.Errorf("failed to copy %s: %w", relativePath, err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finalize destination %s: %w", relativePath, err)
	}

	return nil
}

// Creates a new SyncEngine instance.
func NewSyncEngine(localProvider storage.StorageProvider, remoteProvider storage.StorageProvider) (*SyncEngine, error) {
	// Initialize fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}
	return &SyncEngine{
		localProvider:  localProvider,
		remoteProvider: remoteProvider,
		watcher:        watcher,
		localMap:       make(map[string]models.FileMetadata),
		remoteMap:      make(map[string]models.FileMetadata),
	}, nil
}

// Sets a callback function to be called on sync events.
func (s *SyncEngine) SetEventCallback(callback func(eventType, filePath, direction, message string)) {
	s.eventCallback = callback
}

// Starts the synchronization engine.
func (s *SyncEngine) Run() error {
	if err := s.ensureFolderExists(); err != nil {
		return err
	}
	if err := s.buildInitialState(); err != nil {
		return err
	}
	if err := s.reconcile(); err != nil {
		return err
	}
	// Start the watcher in a separate goroutine
	// This allows the Run method to return immediately after setup.
	go func() {
		if err := s.startWatcher(); err != nil {
			log.Printf("Watcher error: %v\n", err)
		}
	}()
	return nil
}

// Ensures that the local and remote folders exist.
func (s *SyncEngine) ensureFolderExists() error {
	// 0755 is a common permission setting for directories (read/write/execute for owner, read/execute for group and others)
	if err := os.MkdirAll(s.localProvider.GetPath(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(s.remoteProvider.GetPath(), 0755); err != nil {
		return err
	}
	return nil
}

// Builds the initial state maps for local and remote storage.
func (s *SyncEngine) buildInitialState() error {
	localMap, err := s.localProvider.BuildStateMap()
	if err != nil {
		return err
	}
	s.localMap = localMap
	log.Printf("...Local map built with %d files.", len(s.localMap))

	log.Println("Building initial state map for Remote...")
	remoteMap, err := s.remoteProvider.BuildStateMap()
	if err != nil {
		return fmt.Errorf("failed to build remote state map: %w", err)
	}
	s.remoteMap = remoteMap
	log.Printf("...Remote map built with %d files.", len(s.remoteMap))

	return nil
}

// Reconciles differences between local and remote storage.
func (s *SyncEngine) reconcile() error {
	// Lock the state maps during reconciliation
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for files that exist locally but not remotely
	for relPath, localMeta := range s.localMap {
		remoteMeta, existsInRemote := s.remoteMap[relPath]

		if !existsInRemote {
			log.Printf("File %s exists locally but not remotely. Copying to remote...\n", relPath)
			if err := copyFile(s.localProvider, s.remoteProvider, relPath, localMeta.ModTime); err != nil {
				return fmt.Errorf("error copying file %s to remote: %w", relPath, err)
			}
			s.remoteMap[relPath] = localMeta
			log.Printf("File %s copied to remote successfully.\n", relPath)
			continue
		}
		// If file exists in both, compare hashes
		if localMeta.Hash != remoteMeta.Hash {
			// Determine which file is newer based on modification time
			if localMeta.ModTime.After(remoteMeta.ModTime) {
				log.Printf("File %s is newer locally. Updating remote file...\n", relPath)
				if err := copyFile(s.localProvider, s.remoteProvider, relPath, localMeta.ModTime); err != nil {
					return fmt.Errorf("error updating file %s to remote: %w", relPath, err)
				}
				s.remoteMap[relPath] = localMeta
				log.Printf("File %s updated successfully.\n", relPath)
			} else {
				log.Printf("File %s is newer remotely. Updating local file...\n", relPath)
				if err := copyFile(s.remoteProvider, s.localProvider, relPath, remoteMeta.ModTime); err != nil {
					return fmt.Errorf("error updating file %s to local: %w", relPath, err)
				}
				s.localMap[relPath] = remoteMeta
				log.Printf("File %s updated successfully.\n", relPath)
			}
		}
	}

	// Check for files that exist remotely but not locally
	for relPath, remoteMeta := range s.remoteMap {
		if _, existsInLocal := s.localMap[relPath]; !existsInLocal {
			log.Printf("File %s exists remotely but not locally. Copying to local...\n", relPath)
			if err := copyFile(s.remoteProvider, s.localProvider, relPath, remoteMeta.ModTime); err != nil {
				return fmt.Errorf("error copying file %s to local: %w", relPath, err)
			}
			s.localMap[relPath] = remoteMeta
			log.Printf("File %s copied to local successfully.\n", relPath)
		}
	}

	log.Println("Reconciliation complete.")
	return nil
}

// Starts the file system watcher to monitor changes.
func (s *SyncEngine) startWatcher() error {
	log.Println("Starting file system watcher...")
	defer s.watcher.Close()
	// Add local and remote root paths to the watcher
	if err := s.addWatcherPath(s.localProvider.GetPath()); err != nil {
		log.Printf("Error adding local path to watcher: %v\n", err)
	}
	if err := s.addWatcherPath(s.remoteProvider.GetPath()); err != nil {
		log.Printf("Error adding remote path to watcher: %v\n", err)
	}
	// Listen for events
	for {
		select {
		case event, ok := <-s.watcher.Events:
			// If the channel is closed, exit the goroutine
			if !ok {
				log.Println("Watcher event channel closed.")
				return nil
			}
			s.handleEvent(event)
		case err, ok := <-s.watcher.Errors:
			// If the channel is closed, exit the goroutine
			if !ok {
				log.Println("Watcher error channel closed.")
				return nil
			}
			log.Printf("Watcher error: %v\n", err)
		}
	}
}

// Adds a path and its subdirectories to the watcher.
func (s *SyncEngine) addWatcherPath(rootPath string) error {
	// Walk through the directory tree and add each directory to the watcher
	return filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Add directory to watcher
		if d.IsDir() {
			if err := s.watcher.Add(path); err != nil {
				log.Printf("Error adding path %s to watcher: %v\n", path, err)
			} else {
				log.Printf("Watching directory: %s\n", path)
			}
		}
		return nil
	})
}

// Handles file system events.
func (s *SyncEngine) handleEvent(event fsnotify.Event) error {
	log.Printf("Event received: %s | Operation: %s\n", event.Name, event.Op)

	// Check if sync is paused
	s.pauseMu.RLock()
	paused := s.isPaused
	s.pauseMu.RUnlock()

	if paused {
		log.Printf("Sync paused, ignoring event for: %s\n", event.Name)
		return nil
	}
	// Determine if the event is from local or remote provider
	localRoot := s.localProvider.GetPath()
	remoteRoot := s.remoteProvider.GetPath()

	var srcProvider, dstProvider storage.StorageProvider
	var srcMap, dstMap *map[string]models.FileMetadata
	var isLocalEvent bool

	log.Printf("Path check - event: '%s' | local root: '%s' | remote root: '%s'\n", event.Name, localRoot, remoteRoot)

	// Determine if the event is from local or remote provider
	if strings.HasPrefix(event.Name, localRoot) {
		isLocalEvent = true
		srcProvider, dstProvider = s.localProvider, s.remoteProvider
		srcMap, dstMap = &s.localMap, &s.remoteMap
		log.Printf("Matched as local event\n")
	} else if strings.HasPrefix(event.Name, remoteRoot) {
		isLocalEvent = false
		srcProvider, dstProvider = s.remoteProvider, s.localProvider
		srcMap, dstMap = &s.remoteMap, &s.localMap
		log.Printf("Matched as remote event\n")
	} else {
		log.Printf("Unrecognized event source: %s (doesn't match local or remote path)\n", event.Name)
		return fmt.Errorf("unrecognized event source: %s", event.Name)
	}

	// Get the relative path of the event with respect to the source provider's root path
	relPath, err := filepath.Rel(srcProvider.GetPath(), event.Name)
	if err != nil {
		return fmt.Errorf("error getting relative path for event %s: %w", event.Name, err)
	}
	relPath = filepath.ToSlash(relPath)

	// Handle directory creation events
	if event.Op&fsnotify.Create == fsnotify.Create {
		// Check if the created entity is a directory
		info, err := os.Stat(event.Name)
		if err == nil && info.IsDir() {
			log.Printf("Directory created: %s\n", event.Name)
			s.watcher.Add(event.Name)
			if err := dstProvider.EnsureDir(relPath); err != nil {
				log.Printf("error replicating directory %s: %v\n", relPath, err)
			}
			return nil
		}
	}

	// Handle file deletion or renaming events
	if event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename {
		s.mu.Lock()
		defer s.mu.Unlock()
		if event.Op&fsnotify.Rename == fsnotify.Rename && event.Op&fsnotify.Remove != fsnotify.Remove {
			log.Printf("File moved or renamed: %s\n", event.Name)
		} else {
			log.Printf("File deleted: %s\n", event.Name)
		}
		// Remove from both maps
		delete(*srcMap, relPath)
		delete(*dstMap, relPath)

		if err := dstProvider.DeleteFile(relPath); err != nil {
			log.Printf("error deleting file %s: %v\n", relPath, err)
		}

		if s.eventCallback != nil {
			direction := "remote_to_local"
			if isLocalEvent {
				direction = "local_to_remote"
			}
			eventType := "delete"
			message := fmt.Sprintf("File deleted: %s", relPath)
			if event.Op&fsnotify.Rename == fsnotify.Rename && event.Op&fsnotify.Remove != fsnotify.Remove {
				eventType = "move"
				message = fmt.Sprintf("File moved or renamed: %s", relPath)
			}
			s.eventCallback(eventType, relPath, direction, message)
		}
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("Checking event type: %s | CREATE=%v WRITE=%v CHMOD=%v\n", event.Op,
		event.Op&fsnotify.Create == fsnotify.Create,
		event.Op&fsnotify.Write == fsnotify.Write,
		event.Op&fsnotify.Chmod == fsnotify.Chmod)

	// Handle file creation or modification events
	if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Chmod == fsnotify.Chmod {
		srcMeta, err := srcProvider.GetMetadata(relPath)
		// If the file no longer exists, it might have been deleted after the event was fired
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("File %s no longer exists.\n", event.Name)
				if err := dstProvider.DeleteFile(relPath); err != nil {
					log.Printf("error deleting file %s: %v\n", relPath, err)
				}
				// Remove from both maps
				delete(*srcMap, relPath)
				delete(*dstMap, relPath)
			} else {
				log.Printf("error getting metadata for file %s: %v\n", event.Name, err)
			}
			return nil
		}
		// Update the source map with the latest metadata
		dstMeta, existsInDst := (*dstMap)[relPath]
		if existsInDst && srcMeta.Hash == dstMeta.Hash {
			// No action needed, files are identical
			(*srcMap)[relPath] = srcMeta
			(*dstMap)[relPath] = dstMeta
			return nil
		}
		// If the source file is newer, copy it to the destination
		if !existsInDst || srcMeta.ModTime.After(dstMeta.ModTime) {
			direction := "remote_to_local"
			if isLocalEvent {
				direction = "local_to_remote"
			}
			log.Printf("%s sync for %s\n", direction, relPath)
			// Copy the source file to the destination
			if err := copyFile(srcProvider, dstProvider, relPath, srcMeta.ModTime); err != nil {
				log.Printf("error syncing file %s: %v\n", relPath, err)
				return nil
			}
			// Update the destination map with the latest metadata
			(*srcMap)[relPath] = srcMeta
			(*dstMap)[relPath] = srcMeta

			if s.eventCallback != nil {
				s.eventCallback("sync", relPath, direction, fmt.Sprintf("File synced: %s", relPath))
			}
		} else { // Source file is older than destination
			log.Printf("Stale write detected for file %s; ignoring.\n", relPath)
			// Revert the change by copying the destination file back to the source
			if err := copyFile(dstProvider, srcProvider, relPath, dstMeta.ModTime); err != nil {
				log.Printf("error reverting stale write for file %s: %v\n", relPath, err)
				return nil
			}
			// Update the destination map with the latest metadata
			(*srcMap)[relPath] = dstMeta
			(*dstMap)[relPath] = dstMeta
		}
	}
	return nil
}

// Returns the count of files in the local storage.
func (s *SyncEngine) GetLocalFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.localMap)
}

// Returns the count of files in the remote storage.
func (s *SyncEngine) GetRemoteFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.remoteMap)
}

// Returns a list of all synchronized files with their details.
func (s *SyncEngine) GetFileList() []FileInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fileSet := make(map[string]FileInfo)

	for relPath, meta := range s.localMap {
		fileSet[relPath] = FileInfo{
			RelativePath: relPath,
			Hash:         meta.Hash,
			ModTime:      meta.ModTime.Format("2006-01-02 15:04:05"),
			Location:     "local",
		}
	}

	for relPath, meta := range s.remoteMap {
		if existing, exists := fileSet[relPath]; exists {
			if existing.Hash == meta.Hash {
				existing.Location = "both"
				fileSet[relPath] = existing
			}
		} else {
			fileSet[relPath] = FileInfo{
				RelativePath: relPath,
				Hash:         meta.Hash,
				ModTime:      meta.ModTime.Format("2006-01-02 15:04:05"),
				Location:     "remote",
			}
		}
	}

	// Convert the map to a slice because maps do not guarantee order
	// order is important for consistent API responses
	files := make([]FileInfo, 0, len(fileSet))
	for _, info := range fileSet {
		files = append(files, info)
	}

	return files
}

// Pauses the synchronization engine.
func (s *SyncEngine) Pause() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	s.isPaused = true
	log.Println("Sync engine paused")
}

// Resumes the synchronization engine.
func (s *SyncEngine) Resume() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	s.isPaused = false
	log.Println("Sync engine resumed")
}

// Checks if the synchronization engine is paused.
func (s *SyncEngine) IsPaused() bool {
	s.pauseMu.RLock()
	defer s.pauseMu.RUnlock()
	return s.isPaused
}

// Manually triggers a synchronization process.
func (s *SyncEngine) ManualSync() error {
	// s.pauseMu.RLock()
	// paused := s.isPaused
	// s.pauseMu.RUnlock()

	// if paused {
	// 	log.Println("Cannot perform manual sync while paused")
	// 	return fmt.Errorf("sync engine is paused")
	// }

	log.Println("Starting manual sync...")

	// Rebuild state maps to catch any changes
	if err := s.buildInitialState(); err != nil {
		return fmt.Errorf("failed to rebuild state: %w", err)
	}

	// Run reconciliation
	if err := s.reconcile(); err != nil {
		return fmt.Errorf("failed to reconcile: %w", err)
	}

	log.Println("Manual sync completed successfully")
	return nil
}

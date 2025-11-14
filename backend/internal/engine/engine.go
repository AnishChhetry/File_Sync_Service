package engine

import (
	"backend/internal/config"
	"backend/internal/models"
	"backend/internal/storage"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
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

	jobs             chan queuedEvent
	workerCount      int
	perFileLocks     sync.Map
	debounceMu       sync.Mutex
	pendingEvents    map[string]time.Time
	debounceInterval time.Duration
	workerWG         sync.WaitGroup
	watcherWG        sync.WaitGroup
	stopCh           chan struct{}
	stopOnce         sync.Once
}

// Represents a file system event queued for processing.
type queuedEvent struct {
	raw     fsnotify.Event
	isLocal bool
	relPath string
}

// Represents information about a synchronized file.
type FileInfo struct {
	RelativePath string `json:"relativePath"`
	Hash         string `json:"hash"`
	ModTime      string `json:"modTime"`
	Location     string `json:"location"`
}

// Creates a new SyncEngine instance.
func NewSyncEngine(localProvider storage.StorageProvider, remoteProvider storage.StorageProvider) (*SyncEngine, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	wc := runtime.NumCPU()
	switch {
	case wc < 2:
		wc = 2
	case wc > 8:
		wc = 8
	}

	return &SyncEngine{
		localProvider:  localProvider,
		remoteProvider: remoteProvider,
		watcher:        watcher,
		localMap:       make(map[string]models.FileMetadata),
		remoteMap:      make(map[string]models.FileMetadata),
		jobs:           make(chan queuedEvent, config.DefaultJobBufferSize),
		workerCount:    wc,
		pendingEvents:  make(map[string]time.Time),
		stopCh:         make(chan struct{}),
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

	for i := 0; i < s.workerCount; i++ {
		s.workerWG.Add(1)
		go s.worker()
	}

	if err := s.startWatcher(); err != nil {
		s.Stop()
		return err
	}
	return nil
}

// Signals the engine to shut down and waits for workers to finish.
func (s *SyncEngine) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		if s.watcher != nil {
			if err := s.watcher.Close(); err != nil {
				log.Printf("watcher close error: %v\n", err)
			}
		}
		s.watcherWG.Wait()
		close(s.jobs)
		s.workerWG.Wait()
	})
}

// Starts the file system watcher to monitor changes.
func (s *SyncEngine) startWatcher() error {
	log.Println("Starting file system watcher...")

	if err := s.addWatcherPath(s.localProvider.GetPath()); err != nil {
		log.Printf("Error adding local path to watcher: %v\n", err)
	}
	if err := s.addWatcherPath(s.remoteProvider.GetPath()); err != nil {
		log.Printf("Error adding remote path to watcher: %v\n", err)
	}

	s.watcherWG.Add(1)
	go func() {
		defer s.watcherWG.Done()
		for {
			select {
			case <-s.stopCh:
				return
			case event, ok := <-s.watcher.Events:
				if !ok {
					log.Println("Watcher events channel closed")
					return
				}
				qe, ok := s.categorizeEvent(event)
				if !ok {
					continue
				}
				select {
				case s.jobs <- qe:
				default:
					log.Println("Job channel full, dropping event:", event)
				}
			case err, ok := <-s.watcher.Errors:
				if !ok {
					log.Println("Watcher errors channel closed")
					return
				}
				if err != nil {
					log.Printf("Watcher error: %v\n", err)
				}
			}
		}
	}()

	return nil
}

// Categorizes a file system event.
func (s *SyncEngine) categorizeEvent(event fsnotify.Event) (queuedEvent, bool) {
	if event.Name == "" {
		return queuedEvent{}, false
	}

	isLocal, rel := s.whichSideAndRel(event.Name)
	if rel == "" {
		return queuedEvent{}, false
	}

	qe := queuedEvent{raw: event, isLocal: isLocal, relPath: rel}
	if !s.shouldEnqueue(qe) {
		return queuedEvent{}, false
	}

	return qe, true
}

// Adds a path and its subdirectories to the watcher.
func (s *SyncEngine) addWatcherPath(rootPath string) error {
	return filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
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
			ModTime:      meta.ModTime.Format("YYYY/MM/DD HH:MM:SS"),
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
				ModTime:      meta.ModTime.Format("YYYY/MM/DD HH:MM:SS"),
				Location:     "remote",
			}
		}
	}

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

	log.Println("Starting manual sync...")

	if err := s.buildInitialState(); err != nil {
		return fmt.Errorf("failed to rebuild state: %w", err)
	}
	if err := s.reconcile(); err != nil {
		return fmt.Errorf("failed to reconcile: %w", err)
	}

	log.Println("Manual sync completed successfully")
	return nil
}

// Handles missing files.
func (s *SyncEngine) handleMissingFile(relPath string, isLocal bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, dstProvider := s.getProviders(isLocal)
	srcMap, dstMap := s.getStateMaps(isLocal)

	delete(*srcMap, relPath)
	delete(*dstMap, relPath)

	if err := dstProvider.DeleteFile(relPath); err != nil && !os.IsNotExist(err) {
		log.Printf("error deleting file %s: %v\n", relPath, err)
	}

	if s.eventCallback != nil {
		direction := getDirection(isLocal)
		s.eventCallback("delete", relPath, direction, fmt.Sprintf("File deleted: %s", relPath))
	}
}

// Synchronizes a directory.
func (s *SyncEngine) syncDirectory(relPath string, isLocal bool) error {
	_, dstProvider := s.getProviders(isLocal)
	srcMap, dstMap := s.getStateMaps(isLocal)

	if err := dstProvider.EnsureDir(relPath); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", relPath, err)
	}

	now := time.Now()
	meta := models.FileMetadata{RelativePath: relPath, Hash: "", ModTime: now}
	(*srcMap)[relPath] = meta
	(*dstMap)[relPath] = meta

	if s.eventCallback != nil {
		direction := getDirection(isLocal)
		s.eventCallback("sync", relPath, direction, fmt.Sprintf("Directory synced: %s", relPath))
	}

	return nil
}

// Synchronizes a file.
func (s *SyncEngine) syncFile(event fsnotify.Event, isLocal bool, relPath string) error {
	srcProvider, dstProvider := s.getProviders(isLocal)
	srcMap, dstMap := s.getStateMaps(isLocal)

	srcMeta, err := srcProvider.GetMetadata(relPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("File %s no longer exists\n", event.Name)
			s.handleMissingFile(relPath, isLocal)
			return nil
		}
		return fmt.Errorf("error getting metadata for %s: %w", event.Name, err)
	}

	dstMeta, existsInDst := (*dstMap)[relPath]
	if existsInDst && srcMeta.Hash == dstMeta.Hash {
		(*srcMap)[relPath] = srcMeta
		(*dstMap)[relPath] = dstMeta
		return nil
	}

	if !existsInDst || srcMeta.ModTime.After(dstMeta.ModTime) {
		return s.syncFileToDestination(srcProvider, dstProvider, srcMap, dstMap, relPath, srcMeta, isLocal)
	}

	return s.handleFileConflict(relPath, isLocal)
}

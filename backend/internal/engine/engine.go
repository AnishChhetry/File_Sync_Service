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

type FileInfo struct {
	RelativePath string `json:"relativePath"`
	Hash         string `json:"hash"`
	ModTime      string `json:"modTime"`
	Location     string `json:"location"`
}

func NewSyncEngine(localProvider storage.StorageProvider, remoteProvider storage.StorageProvider) (*SyncEngine, error) {
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

func (s *SyncEngine) SetEventCallback(callback func(eventType, filePath, direction, message string)) {
	s.eventCallback = callback
}

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
	go func() {
		if err := s.startWatcher(); err != nil {
			log.Printf("Watcher error: %v\n", err)
		}
	}()
	return nil
}
func (s *SyncEngine) ensureFolderExists() error {
	if err := os.MkdirAll(s.localProvider.GetPath(), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(s.remoteProvider.GetPath(), 0755); err != nil {
		return err
	}
	return nil
}
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

func (s *SyncEngine) reconcile() error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

		if localMeta.Hash != remoteMeta.Hash {
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

func (s *SyncEngine) startWatcher() error {
	log.Println("Starting file system watcher...")
	defer s.watcher.Close()
	if err := s.addWatcherPath(s.localProvider.GetPath()); err != nil {
		log.Printf("Error adding local path to watcher: %v\n", err)
	}
	if err := s.addWatcherPath(s.remoteProvider.GetPath()); err != nil {
		log.Printf("Error adding remote path to watcher: %v\n", err)
	}
	for {
		select {
		case event, ok := <-s.watcher.Events:
			if !ok {
				log.Println("Watcher event channel closed.")
				return nil
			}
			s.handleEvent(event)
		case err, ok := <-s.watcher.Errors:
			if !ok {
				log.Println("Watcher error channel closed.")
				return nil
			}
			log.Printf("Watcher error: %v\n", err)
		}
	}
}

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
	localRoot := s.localProvider.GetPath()
	remoteRoot := s.remoteProvider.GetPath()

	var srcProvider, dstProvider storage.StorageProvider
	var srcMap, dstMap *map[string]models.FileMetadata
	var isLocalEvent bool

	log.Printf("Path check - event: '%s' | local root: '%s' | remote root: '%s'\n", event.Name, localRoot, remoteRoot)

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

	relPath, err := filepath.Rel(srcProvider.GetPath(), event.Name)
	if err != nil {
		return fmt.Errorf("error getting relative path for event %s: %w", event.Name, err)
	}
	relPath = filepath.ToSlash(relPath)

	if event.Op&fsnotify.Create == fsnotify.Create {
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

	if event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename {
		s.mu.Lock()
		defer s.mu.Unlock()
		if event.Op&fsnotify.Rename == fsnotify.Rename && event.Op&fsnotify.Remove != fsnotify.Remove {
			log.Printf("File moved or renamed: %s\n", event.Name)
		} else {
			log.Printf("File deleted: %s\n", event.Name)
		}
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

	if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Chmod == fsnotify.Chmod {
		srcMeta, err := srcProvider.GetMetadata(relPath)
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("File %s no longer exists.\n", event.Name)
				if err := dstProvider.DeleteFile(relPath); err != nil {
					log.Printf("error deleting file %s: %v\n", relPath, err)
				}
				delete(*srcMap, relPath)
				delete(*dstMap, relPath)
			} else {
				log.Printf("error getting metadata for file %s: %v\n", event.Name, err)
			}
			return nil
		}
		dstMeta, existsInDst := (*dstMap)[relPath]
		if existsInDst && srcMeta.Hash == dstMeta.Hash {
			(*srcMap)[relPath] = srcMeta
			(*dstMap)[relPath] = dstMeta
			return nil
		}

		if !existsInDst || srcMeta.ModTime.After(dstMeta.ModTime) {
			direction := "remote_to_local"
			if isLocalEvent {
				direction = "local_to_remote"
			}
			log.Printf("%s sync for %s\n", direction, relPath)
			if err := copyFile(srcProvider, dstProvider, relPath, srcMeta.ModTime); err != nil {
				log.Printf("error syncing file %s: %v\n", relPath, err)
				return nil
			}
			(*srcMap)[relPath] = srcMeta
			(*dstMap)[relPath] = srcMeta

			if s.eventCallback != nil {
				s.eventCallback("sync", relPath, direction, fmt.Sprintf("File synced: %s", relPath))
			}
		} else {
			log.Printf("Stale write detected for file %s; ignoring.\n", relPath)
			if err := copyFile(dstProvider, srcProvider, relPath, dstMeta.ModTime); err != nil {
				log.Printf("error reverting stale write for file %s: %v\n", relPath, err)
				return nil
			}
			(*srcMap)[relPath] = dstMeta
			(*dstMap)[relPath] = dstMeta
		}
	}
	return nil
}

func (s *SyncEngine) GetLocalFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.localMap)
}

func (s *SyncEngine) GetRemoteFileCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.remoteMap)
}

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

	files := make([]FileInfo, 0, len(fileSet))
	for _, info := range fileSet {
		files = append(files, info)
	}

	return files
}

func (s *SyncEngine) Pause() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	s.isPaused = true
	log.Println("Sync engine paused")
}

func (s *SyncEngine) Resume() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	s.isPaused = false
	log.Println("Sync engine resumed")
}

func (s *SyncEngine) IsPaused() bool {
	s.pauseMu.RLock()
	defer s.pauseMu.RUnlock()
	return s.isPaused
}

func (s *SyncEngine) ManualSync() error {
	s.pauseMu.RLock()
	paused := s.isPaused
	s.pauseMu.RUnlock()

	if paused {
		log.Println("Cannot perform manual sync while paused")
		return fmt.Errorf("sync engine is paused")
	}

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

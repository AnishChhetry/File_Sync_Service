package engine

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/storage"
	"backend/internal/models"
)

// Identifies if event is from local or remote and returns relative path.
func (s *SyncEngine) determineEventSource(absPath string) (bool, string, error) {
	cleanPath := filepath.Clean(absPath)
	localRoot := filepath.Clean(s.localProvider.GetPath())
	remoteRoot := filepath.Clean(s.remoteProvider.GetPath())

	// Try local root
	rel, err := filepath.Rel(localRoot, cleanPath)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return true, filepath.ToSlash(rel), nil
	}

	// Try remote root
	rel, err = filepath.Rel(remoteRoot, cleanPath)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return false, filepath.ToSlash(rel), nil
	}

	return false, "", fmt.Errorf("unrecognized event source: %s", absPath)
}

// Returns sync direction string based on event source.
func getDirection(isLocal bool) string {
	if isLocal {
		return "local_to_remote"
	}
	return "remote_to_local"
}

// Returns source and destination providers based on event source.
func (s *SyncEngine) getProviders(isLocal bool) (storage.StorageProvider, storage.StorageProvider) {
	if isLocal {
		return s.localProvider, s.remoteProvider
	}
	return s.remoteProvider, s.localProvider
}

// Returns source and destination state maps based on event source.
func (s *SyncEngine) getStateMaps(isLocal bool) (*map[string]models.FileMetadata, *map[string]models.FileMetadata) {
	if isLocal {
		return &s.localMap, &s.remoteMap
	}
	return &s.remoteMap, &s.localMap
}

// Determines which side (local or remote) an absolute path belongs to and returns the relative path.
func (s *SyncEngine) whichSideAndRel(absPath string) (bool, string) {
	cleanPath := filepath.Clean(absPath)
	localRoot := filepath.Clean(s.localProvider.GetPath())
	remoteRoot := filepath.Clean(s.remoteProvider.GetPath())

	resolve := func(root string) (bool, string) {
		rel, err := filepath.Rel(root, cleanPath)
		if err != nil {
			return false, ""
		}
		if rel == "." {
			return true, ""
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return false, ""
		}
		return true, filepath.ToSlash(rel)
	}

	if ok, rel := resolve(localRoot); ok {
		return true, rel
	}
	if ok, rel := resolve(remoteRoot); ok {
		return false, rel
	}

	log.Printf("unable to map path %s to local (%s) or remote (%s) roots", absPath, localRoot, remoteRoot)
	return false, ""
}

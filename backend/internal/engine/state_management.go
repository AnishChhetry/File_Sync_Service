package engine

import (
	"fmt"
	"log"
	"os"
)

// Ensures that the local and remote folders exist.
func (s *SyncEngine) ensureFolderExists() error {
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

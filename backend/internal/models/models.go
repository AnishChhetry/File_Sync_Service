package models

import "time"

// FileMetadata holds metadata information about a file.
type FileMetadata struct {
	RelativePath string
	Hash         string
	ModTime      time.Time
}

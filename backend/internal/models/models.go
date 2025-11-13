package models

import "time"

// Holds metadata information about a file.
type FileMetadata struct {
	RelativePath string
	Hash         string
	ModTime      time.Time
}

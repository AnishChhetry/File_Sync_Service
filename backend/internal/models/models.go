package models

import "time"

type FileMetadata struct {
	RelativePath string
	Hash         string
	ModTime      time.Time
}

package storage

import (
	"backend/internal/models"
	"io"
	"time"
)

// Defines the interface for storage backends.
type StorageProvider interface {
	BuildStateMap() (map[string]models.FileMetadata, error)
	GetReader(relativePath string) (io.ReadCloser, error)
	GetWriter(relativePath string, modTime time.Time) (io.WriteCloser, error)
	GetMetadata(relativePath string) (models.FileMetadata, error)
	DeleteFile(relativePath string) error
	EnsureDir(relativePath string) error
	GetPath() string
}

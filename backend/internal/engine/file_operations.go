package engine

import (
	"backend/internal/storage"
	"fmt"
	"io"
	"time"
)

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

package storage

import (
	"backend/internal/models"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type FileSystemProvider struct {
	rootPath string
}

func NewFileSystemProvider(rootPath string) (*FileSystemProvider, error) {
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %s: %w", rootPath, err)
	}
	return &FileSystemProvider{rootPath: absPath}, nil
}

func (p *FileSystemProvider) BuildStateMap() (map[string]models.FileMetadata, error) {
	stateMap := make(map[string]models.FileMetadata)
	err := filepath.WalkDir(p.rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if len(base) > 0 && base[0] == '.' {
			return nil
		}
		meta, err := p.metadataForAbsolute(path)
		if err != nil {
			return fmt.Errorf("error getting metadata for %s: %w", path, err)
		}
		stateMap[meta.RelativePath] = meta
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking directory %s: %w", p.rootPath, err)
	}
	return stateMap, nil
}

func (p *FileSystemProvider) GetReader(relativePath string) (io.ReadCloser, error) {
	fullPath := filepath.Join(p.rootPath, relativePath)
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", fullPath, err)
	}
	return file, nil
}

func (p *FileSystemProvider) GetMetadata(relativePath string) (models.FileMetadata, error) {
	fullPath := filepath.Join(p.rootPath, relativePath)
	return p.metadataForAbsolute(fullPath)
}

type writerWithModTime struct {
	filePath string
	file     *os.File
	modTime  time.Time
}

func (w *writerWithModTime) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

func (w *writerWithModTime) Close() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	if w.modTime.IsZero() {
		return nil
	}
	if err := os.Chtimes(w.filePath, w.modTime, w.modTime); err != nil {
		return fmt.Errorf("failed to preserve mod time for %s: %w", w.filePath, err)
	}
	return nil
}

func (p *FileSystemProvider) GetWriter(relativePath string, modTime time.Time) (io.WriteCloser, error) {
	fullPath := filepath.Join(p.rootPath, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to ensure directory for %s: %w", fullPath, err)
	}
	file, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", fullPath, err)
	}
	return &writerWithModTime{
		filePath: fullPath,
		file:     file,
		modTime:  modTime,
	}, nil
}

func (p *FileSystemProvider) DeleteFile(relativePath string) error {
	fullPath := filepath.Join(p.rootPath, relativePath)
	if err := os.RemoveAll(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete %s: %w", fullPath, err)
	}
	return nil
}

func (p *FileSystemProvider) EnsureDir(relativePath string) error {
	fullPath := filepath.Join(p.rootPath, relativePath)
	if err := os.MkdirAll(fullPath, 0o755); err != nil {
		return fmt.Errorf("failed to ensure directory %s: %w", fullPath, err)
	}
	return nil
}

func (p *FileSystemProvider) GetPath() string {
	return p.rootPath
}

func (p *FileSystemProvider) metadataForAbsolute(fullPath string) (models.FileMetadata, error) {
	info, err := os.Stat(fullPath)
	if err != nil {
		return models.FileMetadata{}, fmt.Errorf("error stating file %s: %w", fullPath, err)
	}
	relPath, err := filepath.Rel(p.rootPath, fullPath)
	if err != nil {
		return models.FileMetadata{}, fmt.Errorf("error getting relative path for file %s: %w", fullPath, err)
	}
	relPath = filepath.ToSlash(relPath)
	hash, err := hashFile(fullPath)
	if err != nil {
		return models.FileMetadata{}, fmt.Errorf("error computing hash for file %s: %w", fullPath, err)
	}
	return models.FileMetadata{
		RelativePath: relPath,
		Hash:         hash,
		ModTime:      info.ModTime(),
	}, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("error opening file %s: %w", path, err)
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("error reading file %s for hashing: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

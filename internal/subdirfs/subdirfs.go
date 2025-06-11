package subdirfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type subDirFS struct {
	fs.FS
	root string
}

// New creates a new subDirFS instance that wraps the given fs.FS
func New(root string) fs.SubFS {
	return &subDirFS{
		FS:   os.DirFS(root),
		root: root,
	}
}

func (s *subDirFS) Sub(dir string) (fs.FS, error) {
	// Prevent path traversal attacks by removing parent directory references
	subPath := filepath.Join("/", dir)

	newPath := filepath.Clean(filepath.Join(s.root, subPath))
	info, err := os.Stat(newPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", newPath)
	}

	return &subDirFS{FS: os.DirFS(newPath), root: newPath}, nil
}

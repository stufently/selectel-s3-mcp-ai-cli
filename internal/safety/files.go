package safety

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Files struct{ root *os.Root }

func OpenFiles(path string) (*Files, error) {
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, e
	}
	return &Files{root: root}, nil
}
func (f *Files) Close() error { return f.root.Close() }
func filePath(path string) error {
	if !filepath.IsLocal(path) || strings.Contains(path, "\\") {
		return fmt.Errorf("file path must be relative to file root")
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return fmt.Errorf("'..' file path segment is forbidden")
		}
	}
	return nil
}
func (f *Files) Read(path string) (*os.File, error) {
	if e := filePath(path); e != nil {
		return nil, e
	}
	return f.root.Open(path)
}
func (f *Files) Write(path string, r io.Reader) error {
	return f.write(path, r, false)
}

// Replace publishes a completed download over an existing destination. Callers
// must obtain destructive approval before choosing this operation.
func (f *Files) Replace(path string, r io.Reader) error {
	return f.write(path, r, true)
}

func (f *Files) write(path string, r io.Reader, overwrite bool) error {
	if e := filePath(path); e != nil {
		return e
	}
	name := filepath.Join(filepath.Dir(path), ".download-"+hex.EncodeToString(randomBytes()))
	out, e := f.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer func() { _ = f.root.Remove(name) }()
	_, e = io.Copy(out, r)
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if overwrite {
		return f.root.Rename(name, path)
	}
	// Link atomically refuses any existing destination, including a raced file
	// or symlink. The deferred Remove only removes our temporary name.
	return f.root.Link(name, path)
}
func randomBytes() []byte { b := make([]byte, 16); _, _ = rand.Read(b); return b }

package safety

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A reader creates the destination after Write starts, just before publication.
type publishRaceReader struct {
	t    *testing.T
	path string
	done bool
}

func (r *publishRaceReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	if err := os.WriteFile(r.path, []byte("original"), 0600); err != nil {
		r.t.Fatal(err)
	}
	return copy(p, "replacement"), nil
}

func TestWriteNoReplace(t *testing.T) {
	dir := t.TempDir()
	f, err := OpenFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	path := filepath.Join(dir, "target")
	err = f.Write("target", &publishRaceReader{t: t, path: path})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("NO_REPLACE: expected existing-file error, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("NO_REPLACE: target changed: %q err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("NO_REPLACE: temporary file leaked: %v err=%v", entries, err)
	}
	if err = f.Write("fresh", strings.NewReader("new")); err != nil {
		t.Fatal(err)
	}
}

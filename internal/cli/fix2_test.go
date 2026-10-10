package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
)

func TestUploadTreeAddsPrefixSeparator(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	var puts atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		if r.Method != "PUT" {
			t.Errorf("unexpected method %s", r.Method)
			return
		}
		puts.Add(1)
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != "selected" || r.URL.Path != "/bucket/prefix/child/file" {
			t.Errorf("UPLOAD_SEPARATOR: path=%s data=%q err=%v", r.URL.Path, data, err)
		}
	}))
	defer endpoint.Close()
	c, err := s3core.New(config.Config{Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret", Policy: "allow"}, p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child", "file"), []byte("selected"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	err = uploadTree(context.Background(), c, root, []string{"child/file"}, s3core.Args{Bucket: "bucket", Key: "prefix"})
	if err != nil || puts.Load() != 1 {
		t.Fatalf("UPLOAD_SEPARATOR: err=%v puts=%d", err, puts.Load())
	}
}

func TestRecursiveCopyReadsSelectedDirectory(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	var puts atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		if r.Method != "PUT" {
			t.Errorf("unexpected method %s", r.Method)
			return
		}
		puts.Add(1)
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != "selected bytes" || r.URL.Path != "/bucket/prefix/child/file" {
			t.Errorf("RECURSIVE_SELECTED: wrong source or destination: path=%s data=%q err=%v", r.URL.Path, data, err)
		}
	}))
	defer endpoint.Close()
	selected, cwd := t.TempDir(), t.TempDir()
	for _, fixture := range []struct{ dir, data string }{{selected, "selected bytes"}, {cwd, "wrong cwd bytes"}} {
		if err := os.Mkdir(filepath.Join(fixture.dir, "child"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.dir, "child", "file"), []byte(fixture.data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(cwd)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(p.EnvPrefix+"ACCESS_KEY_ID", "fake")
	t.Setenv(p.EnvPrefix+"SECRET_ACCESS_KEY", "fake-secret")
	t.Setenv(p.EnvPrefix+"ENDPOINT", endpoint.URL)
	t.Setenv(p.EnvPrefix+"ALLOW_INSECURE", "1")
	t.Setenv(p.EnvPrefix+"DESTRUCTIVE_POLICY", "allow")
	cmd := New(p, "test", "test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"cp", "--recursive", selected, "s3://bucket/prefix"})
	if err := cmd.Execute(); err != nil || puts.Load() != 1 {
		t.Fatalf("RECURSIVE_SELECTED: err=%v puts=%d", err, puts.Load())
	}
}

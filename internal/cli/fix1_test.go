package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
)

func TestUploadTreeConfinedToRoot(t *testing.T) {
	var puts atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "PUT" {
			puts.Add(1)
		}
		w.WriteHeader(200)
	}))
	defer endpoint.Close()
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := s3core.New(config.Config{Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret", Policy: "allow"}, p)
	if err != nil {
		t.Fatal(err)
	}
	dir, outside := t.TempDir(), t.TempDir()
	if err = os.WriteFile(filepath.Join(outside, "private"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	paths := []string{"child/private"} // Saved walk result; child is then replaced.
	if err = os.Remove(filepath.Join(dir, "child")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, "child")); err != nil {
		t.Fatal(err)
	}
	err = uploadTree(context.Background(), c, root, paths, s3core.Args{Bucket: "bucket", Key: "prefix"})
	if err == nil || !strings.Contains(err.Error(), "escapes from parent") || puts.Load() != 0 {
		t.Fatalf("UPLOAD_ROOT: escaped tree or wrong refusal: err=%v PUTs=%d", err, puts.Load())
	}
}

func TestUploadTreeReadsSelectedDirectory(t *testing.T) {
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
		if r.Method == "PUT" {
			puts.Add(1)
			data, err := io.ReadAll(r.Body)
			if err != nil || string(data) != "selected" || r.URL.Path != "/bucket//prefix/child/file" {
				t.Errorf("UPLOAD_SELECTED: wrong bytes or destination: %q %s err=%v", data, r.URL.Path, err)
			}
		}
		w.WriteHeader(200)
	}))
	defer endpoint.Close()
	c, err := s3core.New(config.Config{Pool: p.DefaultPool, Endpoint: endpoint.URL, AccessKey: "fake", SecretKey: "fake-secret", Policy: "allow"}, p)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "selected")
	if err = os.MkdirAll(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "child", "file"), []byte("selected"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err = os.Rename(dir, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "child", "file"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	err = uploadTree(context.Background(), c, root, []string{"child/file"}, s3core.Args{Bucket: "bucket", Key: "/prefix/"})
	if err != nil || puts.Load() != 1 {
		t.Fatalf("UPLOAD_SELECTED: selected root upload failed: %v puts=%d", err, puts.Load())
	}
}

func TestYesApprovesNonTTYDestructiveOperations(t *testing.T) {
	p, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"rm", "rb", "mv"} {
		t.Run(command, func(t *testing.T) {
			var deletes, copies atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "HEAD":
					if r.URL.Path == "/bucket/target" {
						w.WriteHeader(404)
						return
					}
					w.Header().Set("ETag", `"initial"`)
				case "PUT":
					copies.Add(1)
					_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
				case "DELETE":
					deletes.Add(1)
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
			}))
			defer endpoint.Close()
			t.Setenv("HOME", t.TempDir())
			t.Setenv(p.EnvPrefix+"ACCESS_KEY_ID", "fake")
			t.Setenv(p.EnvPrefix+"SECRET_ACCESS_KEY", "fake-secret")
			t.Setenv(p.EnvPrefix+"ENDPOINT", endpoint.URL)
			t.Setenv(p.EnvPrefix+"ALLOW_INSECURE", "1")
			t.Setenv(p.EnvPrefix+"DESTRUCTIVE_POLICY", "confirm")
			args := []string{command, "--yes", "s3://bucket/key"}
			if command == "rb" {
				args[2] = "s3://bucket"
			}
			if command == "mv" {
				args = append(args, "s3://bucket/target")
			}
			cmd := New(p, "test", "test")
			cmd.SetIn(strings.NewReader(""))
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil || deletes.Load() != 1 {
				t.Fatalf("CLI_YES: approved %s failed or never reached S3: err=%v deletes=%d", command, err, deletes.Load())
			}
			if command == "mv" && copies.Load() != 1 {
				t.Fatalf("CLI_YES: move did not copy: copies=%d", copies.Load())
			}
		})
	}
}

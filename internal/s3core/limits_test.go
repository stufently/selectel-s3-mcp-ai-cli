package s3core

import (
	"context"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAllExistingDestinations(t *testing.T) {
	var heads, writes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			heads.Add(1)
		} else {
			writes.Add(1)
		}
		w.WriteHeader(200)
	}, "allow")
	c.Config.FileRoot = t.TempDir()
	if e := os.WriteFile(filepath.Join(c.Config.FileRoot, "file"), []byte("payload"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, tool := range []string{"s3_put_object", "s3_copy_object", "s3_move_object", "s3_multipart_complete", "s3_upload_file"} {
		_, e := c.Execute(context.Background(), tool, Args{Bucket: "bucket", Key: "exists", SourceBucket: "source", SourceKey: "key", Data: "payload", Path: "file", UploadID: "upload", Parts: []Part{{Number: 1, ETag: "tag"}}}, nil)
		if e == nil || !strings.Contains(e.Error(), "already_exists") {
			t.Fatalf("%s did not reject existing destination: %v", tool, e)
		}
	}
	if heads.Load() != 5 || writes.Load() != 0 {
		t.Fatalf("expected five HEAD and zero writes; heads=%d writes=%d", heads.Load(), writes.Load())
	}
}
func TestHeadPermissionFailure(t *testing.T) {
	var writes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "HEAD" {
			writes.Add(1)
		}
		w.WriteHeader(403)
	}, "allow")
	if _, e := c.Execute(context.Background(), "s3_put_object", Args{Bucket: "bucket", Key: "denied", Data: "payload"}, nil); e == nil || writes.Load() != 0 {
		t.Fatal("HEAD access denial treated as absent target")
	}
}
func TestInlineLimits(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", int(InlineLimit)+1)))
	}, "allow")
	if _, e := c.Execute(context.Background(), "s3_put_object", Args{Bucket: "bucket", Key: "large", Data: strings.Repeat("x", int(InlineLimit)+1)}, nil); e == nil {
		t.Fatal("oversize inline put accepted")
	}
	if requests.Load() != 0 {
		t.Fatal("oversize inline put contacted S3")
	}
	if _, e := c.Execute(context.Background(), "s3_get_object", Args{Bucket: "bucket", Key: "large"}, nil); e == nil {
		t.Fatal("oversize inline get accepted")
	}
}
func TestErrorMapping(t *testing.T) {
	err := &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 429}}, Err: &smithy.GenericAPIError{Code: "RateLimit", Message: "secret-value https://example.test/?X-Amz-Signature=private"}}
	mapped := MapError(err, "secret-value")
	if !mapped.Retryable || mapped.Code != "RateLimit" || strings.Contains(mapped.Message, "secret-value") || strings.Contains(mapped.Message, "private") {
		t.Fatal("error mapping or redaction")
	}
}
func TestPreviewBinary(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-65535" {
			t.Error("preview range missing")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{255, 0, 1})
	}, "allow")
	out, e := c.Execute(context.Background(), "s3_preview_object", Args{Bucket: "bucket", Key: "binary"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if out.(map[string]any)["encoding"] != "base64" {
		t.Fatal("binary preview is not lossless")
	}
}

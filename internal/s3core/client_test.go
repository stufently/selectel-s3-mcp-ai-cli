package s3core

import (
	"context"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func testClient(t *testing.T, h http.HandlerFunc, policy string) *Client {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	c, e := New(config.Config{Pool: p.DefaultPool, Endpoint: server.URL, AccessKey: "testkey", SecretKey: "testsecret", Policy: policy, Allow: map[string]bool{"write": true, "delete": true, "admin": true}}, p)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestConditionalPut(t *testing.T) {
	var headers atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "PUT" && r.Header.Get("If-None-Match") == "*" {
			headers.Add(1)
		}
		w.Header().Set("ETag", "`etag`")
		w.WriteHeader(200)
	}, "confirm")
	_, e := c.Execute(context.Background(), "s3_put_object", Args{Bucket: "bucket", Key: "new", Data: "data"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if headers.Load() != 1 {
		t.Fatal("CONDITIONAL: default put omitted If-None-Match")
	}
}
func TestOverwriteBlocked(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(200) }, "block")
	for _, name := range []string{"s3_put_object", "s3_copy_object", "s3_move_object", "s3_multipart_complete", "s3_upload_file", "s3_download_file"} {
		_, e := c.Execute(context.Background(), name, Args{Bucket: "bucket", Key: "target", SourceBucket: "source", SourceKey: "key", Overwrite: true, Confirm: true, UploadID: "id", Path: "file", Parts: []Part{{Number: 1, ETag: "etag"}}}, nil)
		if e == nil {
			t.Fatalf("OVERWRITE_GATE: %s accepted blocked overwrite", name)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("ZERO_REQUESTS: denied overwrite sent %d requests", requests.Load())
	}
}
func TestGateZeroRequests(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(200) }, "confirm")
	_, e := c.Execute(context.Background(), "s3_delete_object", Args{Bucket: "bucket", Key: "target"}, nil)
	if e == nil || requests.Load() != 0 {
		t.Fatal("ZERO_REQUESTS: confirmation denial contacted S3")
	}
}
func TestMultipartExistingTarget(t *testing.T) {
	var completes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			completes.Add(1)
		}
		w.WriteHeader(200)
	}, "confirm")
	_, e := c.Execute(context.Background(), "s3_multipart_complete", Args{Bucket: "bucket", Key: "exists", UploadID: "id", Parts: []Part{{Number: 1, ETag: "etag"}}}, nil)
	if e == nil || completes.Load() != 0 {
		t.Fatal("MULTIPART_EXISTS: completed over existing target")
	}
}
func TestBucketScopeBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(200) }, "allow")
	c.Config.Prefixes = []string{"bucket/backups/"}
	if e := c.SetScope(); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"s3_get_bucket_config", "s3_put_bucket_versioning", "s3_create_bucket", "s3_delete_bucket", "s3_put_bucket_policy", "s3_put_bucket_cors", "s3_put_object_lock_configuration", "s3_list_objects", "s3_list_object_versions", "s3_list_multipart_uploads"} {
		if _, e := c.Execute(context.Background(), name, Args{Bucket: "bucket", Status: "Enabled", Policy: "{}"}, nil); e == nil {
			t.Fatalf("partial bucket accepted %s", name)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("partial bucket denial contacted S3")
	}
}

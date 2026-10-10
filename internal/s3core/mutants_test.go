package s3core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdditionalDestructiveGates(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(200) }, "confirm")
	for _, tc := range []struct {
		name string
		args Args
	}{
		{"s3_delete_bucket", Args{Bucket: "bucket"}},
		{"s3_put_bucket_versioning", Args{Bucket: "bucket", Status: "Suspended"}},
	} {
		_, err := c.Execute(context.Background(), tc.name, tc.args, nil)
		if err == nil || !strings.Contains(err.Error(), "confirmation_required:") || requests.Load() != 0 {
			t.Fatalf("ADDITIONAL_GATE: %s wrong refusal or reached S3: %v requests=%d", tc.name, err, requests.Load())
		}
	}
	if _, err := c.Execute(context.Background(), "s3_put_bucket_versioning", Args{Bucket: "bucket", Status: "Enabled"}, nil); err != nil || requests.Load() != 1 {
		t.Fatalf("ADDITIONAL_GATE: Enabled unexpectedly gated: %v requests=%d", err, requests.Load())
	}
}

func TestApprovedOverwriteSkipsAbsenceCheck(t *testing.T) {
	var heads, puts atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			heads.Add(1)
		}
		if r.Method == "PUT" {
			puts.Add(1)
			if r.Header.Get("If-None-Match") != "" {
				t.Error("OVERWRITE_ALLOWED: overwrite carried no-replace condition")
			}
		}
		w.WriteHeader(200)
	}, "allow")
	_, err := c.Execute(context.Background(), "s3_put_object", Args{Bucket: "bucket", Key: "exists", Data: "new", Overwrite: true}, nil)
	if err != nil || heads.Load() != 0 || puts.Load() != 1 {
		t.Fatalf("OVERWRITE_ALLOWED: approved overwrite refused: %v heads=%d puts=%d", err, heads.Load(), puts.Load())
	}
}

func TestListPageBounds(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("max-keys") != "1000" {
			t.Errorf("PAGE_SIZE: max-keys=%s, want 1000", r.URL.Query().Get("max-keys"))
		}
		_, _ = fmt.Fprint(w, `<ListBucketResult/>`)
	}, "allow")
	for _, n := range []int32{0, 1000} {
		if _, err := c.Execute(context.Background(), "s3_list_objects", Args{Bucket: "bucket", MaxKeys: n}, nil); err != nil {
			t.Fatalf("PAGE_SIZE: %d refused: %v", n, err)
		}
	}
	for _, n := range []int32{-1, 1001} {
		_, err := c.Execute(context.Background(), "s3_list_objects", Args{Bucket: "bucket", MaxKeys: n}, nil)
		if err == nil || !strings.Contains(err.Error(), "max_keys must be 1..1000") || requests.Load() != 2 {
			t.Fatalf("PAGE_BOUND: %d wrong refusal or reached S3: %v requests=%d", n, err, requests.Load())
		}
	}
}

func TestPresignExpiryBounds(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("presign contacted S3") }, "allow")
	out, err := c.Execute(context.Background(), "s3_presign_get", Args{Bucket: "bucket", Key: "key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := out.(map[string]any)
	u, err := url.Parse(data["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if data["expires_seconds"] != int64(3600) || u.Query().Get("X-Amz-Expires") != "3600" {
		t.Fatalf("PRESIGN_EXPIRY: wrong default: %v", data)
	}
	_, err = c.Execute(context.Background(), "s3_presign_get", Args{Bucket: "bucket", Key: "key", Expires: c.Profile.PresignMaxSeconds + 1}, nil)
	if err == nil || !strings.Contains(err.Error(), "expires_seconds must be 1..") {
		t.Fatalf("PRESIGN_EXPIRY: excessive lifetime accepted or wrong refusal: %v", err)
	}
}

func TestInlinePutLiteralBoundary(t *testing.T) {
	var requests, puts atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "PUT" {
			puts.Add(1)
		}
		w.WriteHeader(200)
	}, "allow")
	if _, err := c.Execute(context.Background(), "s3_put_object", Args{Bucket: "bucket", Key: "key", Data: strings.Repeat("x", 262144)}, nil); err != nil || puts.Load() != 1 {
		t.Fatalf("INLINE_BOUNDARY: 262144 bytes refused: %v puts=%d", err, puts.Load())
	}
	before := requests.Load()
	_, err := c.Execute(context.Background(), "s3_put_object", Args{Bucket: "bucket", Key: "key", Data: strings.Repeat("x", 262145)}, nil)
	if err == nil || !strings.Contains(err.Error(), "inline put exceeds max_bytes") || requests.Load() != before {
		t.Fatalf("INLINE_BOUNDARY: 262145 bytes accepted or wrong refusal: %v", err)
	}
}

func TestScopeAndSourceGuardReasons(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, `<ListBucketResult/>`)
	}, "allow")
	c.Config.Prefixes = []string{"bucket/backups/"}
	if err := c.SetScope(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		args   Args
		reason string
	}{
		{"s3_list_objects", Args{Bucket: "bucket", Prefix: "backups"}, "scope_denied: listing requires prefix backups/"},
		{"s3_delete_bucket", Args{Bucket: "bucket", Key: "backups/a"}, "scope_denied: bucket bucket is partially allowed"},
		{"s3_copy_object", Args{Bucket: "bucket", Key: "backups/a", SourceBucket: "bucket", SourceKey: "backups/a"}, "source and destination must differ"},
		{"s3_move_object", Args{Bucket: "bucket", Key: "backups/a", SourceBucket: "bucket", SourceKey: "backups/a"}, "source and destination must differ"},
	} {
		_, err := c.Execute(context.Background(), tc.name, tc.args, nil)
		if err == nil || !strings.Contains(err.Error(), tc.reason) || requests.Load() != 0 {
			t.Fatalf("GUARD_REASON: %s expected %q: err=%v requests=%d", tc.name, tc.reason, err, requests.Load())
		}
	}
	if _, err := c.Execute(context.Background(), "s3_list_objects", Args{Bucket: "bucket", Prefix: "backups/"}, nil); err != nil || requests.Load() != 1 {
		t.Fatalf("GUARD_REASON: valid listing refused: %v requests=%d", err, requests.Load())
	}
}

func TestDeleteObjectsBoundary(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); _, _ = fmt.Fprint(w, `<DeleteResult/>`) }, "allow")
	objects := make([]ObjectID, 1001)
	for i := range objects {
		objects[i].Key = fmt.Sprintf("key/%d", i)
	}
	_, err := c.Execute(context.Background(), "s3_delete_objects", Args{Bucket: "bucket", Objects: objects}, nil)
	if err == nil || !strings.Contains(err.Error(), "objects must contain 1..1000 entries") || requests.Load() != 0 {
		t.Fatalf("DELETE_BOUNDARY: 1001 accepted or wrong refusal: %v requests=%d", err, requests.Load())
	}
	if _, err = c.Execute(context.Background(), "s3_delete_objects", Args{Bucket: "bucket", Objects: objects[:1000]}, nil); err != nil || requests.Load() != 1 {
		t.Fatalf("DELETE_BOUNDARY: 1000 refused: %v requests=%d", err, requests.Load())
	}
}

func TestConditionalWritesDisabled(t *testing.T) {
	var puts atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "PUT" {
			puts.Add(1)
			if r.Header.Get("If-None-Match") != "" {
				t.Error("CONDITIONAL_DISABLED: disabled capability sent If-None-Match")
			}
		}
		w.WriteHeader(200)
	}, "allow")
	c.Profile.Features["conditional_writes"] = false
	if _, err := c.Execute(context.Background(), "s3_put_object", Args{Bucket: "bucket", Key: "new", Data: "payload"}, nil); err != nil || puts.Load() != 1 {
		t.Fatalf("CONDITIONAL_DISABLED: put failed: %v puts=%d", err, puts.Load())
	}
}

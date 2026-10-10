package s3core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMoveRequiresSourceETag(t *testing.T) {
	var heads, copies, deletes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "HEAD":
			if r.URL.Path != "/source/object" {
				w.WriteHeader(404)
				return
			}
			heads.Add(1) // Deliberately omit ETag.
		case "PUT":
			copies.Add(1)
			_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
		case "DELETE":
			deletes.Add(1)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	}, "allow")
	result, err := c.Execute(context.Background(), "s3_move_object", Args{Bucket: "target", Key: "copy", SourceBucket: "source", SourceKey: "object"}, nil)
	if err == nil || !strings.Contains(err.Error(), "source ETag is required for move") || result != nil || heads.Load() != 1 || copies.Load() != 0 || deletes.Load() != 0 {
		t.Fatalf("MOVE_MISSING_ETAG: wrong refusal or side effects: result=%v err=%v heads=%d copies=%d deletes=%d", result, err, heads.Load(), copies.Load(), deletes.Load())
	}
}

func TestMoveUsesObservedETag(t *testing.T) {
	var heads, copies, deletes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "HEAD":
			if r.URL.Path != "/source/object" {
				w.WriteHeader(404)
				return
			}
			heads.Add(1)
			w.Header().Set("ETag", `"other"`)
		case "PUT":
			copies.Add(1)
			if got := r.Header.Get("X-Amz-Copy-Source-If-Match"); got != `"other"` {
				t.Errorf("MOVE_OBSERVED_ETAG: copy condition=%q want observed ETag", got)
			}
			_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
		case "DELETE":
			deletes.Add(1)
			if r.URL.Path != "/source/object" || r.Header.Get("If-Match") != `"other"` {
				t.Errorf("MOVE_OBSERVED_ETAG: delete path=%s condition=%q", r.URL.Path, r.Header.Get("If-Match"))
			}
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	}, "allow")
	_, err := c.Execute(context.Background(), "s3_move_object", Args{Bucket: "target", Key: "copy", SourceBucket: "source", SourceKey: "object"}, nil)
	if err != nil || heads.Load() != 2 || copies.Load() != 1 || deletes.Load() != 1 {
		t.Fatalf("MOVE_OBSERVED_ETAG: unchanged source failed: err=%v heads=%d copies=%d deletes=%d", err, heads.Load(), copies.Load(), deletes.Load())
	}
}

func TestMoveReportsDeletionFailure(t *testing.T) {
	var copies, deletes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "HEAD":
			if r.URL.Path != "/source/object" {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("ETag", `"other"`)
		case "PUT":
			copies.Add(1)
			_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
		case "DELETE":
			deletes.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `<Error><Code>InternalError</Code><Message>deletion unavailable</Message></Error>`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	}, "allow")
	result, err := c.Execute(context.Background(), "s3_move_object", Args{Bucket: "target", Key: "copy", SourceBucket: "source", SourceKey: "object"}, nil)
	if err == nil || !strings.Contains(err.Error(), "copy succeeded but source deletion failed") || strings.Contains(err.Error(), "source changed during move") || result != nil || copies.Load() != 1 || deletes.Load() == 0 {
		t.Fatalf("MOVE_DELETE_FAILURE: wrong reason: result=%v err=%v copies=%d deletes=%d", result, err, copies.Load(), deletes.Load())
	}
}

func TestDoesNotRetryHTTP408(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusRequestTimeout)
		_, _ = fmt.Fprint(w, `<Error><Code>HTTP408</Code><Message>request timed out</Message></Error>`)
	}, "allow")
	_, err := c.Execute(context.Background(), "s3_list_objects", Args{Bucket: "bucket"}, nil)
	if err == nil || !strings.Contains(err.Error(), "408") || requests.Load() != 1 {
		t.Fatalf("NO_RETRY_408: err=%v requests=%d want one failed request", err, requests.Load())
	}
}

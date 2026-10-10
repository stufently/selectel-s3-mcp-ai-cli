package s3core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMoveKeepsChangedSource(t *testing.T) {
	for _, tc := range []struct {
		name         string
		changed      bool
		deleteStatus int
		wantDeletes  int32
		wantError    bool
	}{
		{"changed", true, 204, 0, true},
		{"unchanged", false, 204, 1, false},
		{"delete_race", false, 412, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var heads, copies, deletes atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "HEAD":
					if r.URL.Path != "/source/object" {
						w.WriteHeader(404)
						return
					}
					etag := `"initial"`
					if heads.Add(1) > 1 && tc.changed {
						etag = `"changed"`
					}
					w.Header().Set("ETag", etag)
				case "PUT":
					copies.Add(1)
					if r.Header.Get("X-Amz-Copy-Source-If-Match") != `"initial"` {
						t.Error("MOVE: copy omitted source ETag condition")
					}
					w.Header().Set("Content-Type", "application/xml")
					_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"copied"</ETag></CopyObjectResult>`)
				case "DELETE":
					deletes.Add(1)
					if r.URL.Path != "/source/object" || r.Header.Get("If-Match") != `"initial"` {
						t.Error("MOVE: delete omitted source ETag condition")
					}
					w.WriteHeader(tc.deleteStatus)
					if tc.deleteStatus == 412 {
						_, _ = fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
					}
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
			}, "allow")
			result, err := c.Execute(context.Background(), "s3_move_object", Args{Bucket: "target", Key: "copy", SourceBucket: "source", SourceKey: "object"}, nil)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "source changed during move; copy kept") || result != nil {
					t.Fatalf("MOVE: changed source reported success or wrong error: result=%v err=%v", result, err)
				}
			} else if err != nil {
				t.Fatalf("MOVE: unchanged source failed: %v", err)
			}
			if heads.Load() != 2 || copies.Load() != 1 || deletes.Load() != tc.wantDeletes {
				t.Fatalf("MOVE: heads=%d copies=%d deletes=%d want deletes=%d", heads.Load(), copies.Load(), deletes.Load(), tc.wantDeletes)
			}
		})
	}
}

func TestRetriesHTTP429(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) <= 2 {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(429)
			_, _ = fmt.Fprint(w, `<Error><Code>RateLimit</Code><Message>slow down</Message></Error>`)
			return
		}
		_, _ = fmt.Fprint(w, `<ListBucketResult/>`)
	}, "allow")
	_, err := c.Execute(context.Background(), "s3_list_objects", Args{Bucket: "bucket"}, nil)
	if err != nil || requests.Load() != 3 {
		t.Fatalf("RETRY_429: err=%v requests=%d, want successful third request", err, requests.Load())
	}
}

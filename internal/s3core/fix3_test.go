package s3core

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMoveETagPassthrough(t *testing.T) {
	for _, etag := range []string{`"abc"`, `"AbC"`, `""`, `W/"x"`, `"a-3"`} {
		t.Run(etag, func(t *testing.T) {
			var heads, copies, deletes atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodHead:
					if r.URL.Path != "/source/object" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					heads.Add(1)
					w.Header().Set("ETag", etag)
				case http.MethodPut:
					copies.Add(1)
					if got := r.Header.Get("X-Amz-Copy-Source-If-Match"); got != etag {
						t.Errorf("MOVE_ETAG: copy condition=%q want=%q", got, etag)
					}
					_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
				case http.MethodDelete:
					deletes.Add(1)
					if got := r.Header.Get("If-Match"); r.URL.Path != "/source/object" || got != etag {
						t.Errorf("MOVE_ETAG: delete path=%s condition=%q want=%q", r.URL.Path, got, etag)
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
			}, "allow")
			_, err := c.Execute(context.Background(), "s3_move_object", Args{Bucket: "target", Key: "copy", SourceBucket: "source", SourceKey: "object"}, nil)
			if err != nil || heads.Load() != 2 || copies.Load() != 1 || deletes.Load() != 1 {
				t.Fatalf("MOVE_ETAG: etag=%q err=%v heads=%d copies=%d deletes=%d", etag, err, heads.Load(), copies.Load(), deletes.Load())
			}
		})
	}
}

func TestMoveDeleteStatusMatrix(t *testing.T) {
	for _, status := range []int{400, 403, 404, 409, 412, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var heads, copies, deletes atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodHead:
					if r.URL.Path != "/source/object" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					heads.Add(1)
					w.Header().Set("ETag", `"source"`)
				case http.MethodPut:
					copies.Add(1)
					_, _ = fmt.Fprint(w, `<CopyObjectResult><ETag>"copy"</ETag></CopyObjectResult>`)
				case http.MethodDelete:
					deletes.Add(1)
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(status)
					_, _ = fmt.Fprint(w, `<Error><Code>TestStatus</Code><Message>deletion unavailable</Message></Error>`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
			}, "allow")
			result, err := c.Execute(context.Background(), "s3_move_object", Args{Bucket: "target", Key: "copy", SourceBucket: "source", SourceKey: "object"}, nil)
			want, unwanted := "copy succeeded but source deletion failed", "source changed during move; copy kept"
			if status == http.StatusPreconditionFailed {
				want, unwanted = unwanted, want
			}
			if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), unwanted) || result != nil {
				t.Fatalf("MOVE_DELETE_STATUS: status=%d result=%v err=%v want=%q", status, result, err, want)
			}
			wantDeletes := int32(1)
			if status == http.StatusInternalServerError {
				wantDeletes = 3
			}
			if heads.Load() != 2 || copies.Load() != 1 || deletes.Load() != wantDeletes {
				t.Fatalf("MOVE_DELETE_STATUS: heads=%d copies=%d deletes=%d want deletes=%d", heads.Load(), copies.Load(), deletes.Load(), wantDeletes)
			}
		})
	}
}

func TestRetryStatusMatrix(t *testing.T) {
	for _, tc := range []struct {
		status    int
		retryable bool
	}{
		{400, false}, {403, false}, {404, false}, {408, false}, {409, false}, {412, false},
		{429, true}, {500, true}, {502, true}, {503, true}, {504, true},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			var requests atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				if requests.Add(1) == 1 {
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, `<Error><Code>TestStatus</Code><Message>status matrix</Message></Error>`)
					return
				}
				_, _ = fmt.Fprint(w, `<ListBucketResult/>`)
			}, "allow")
			_, err := c.Execute(context.Background(), "s3_list_objects", Args{Bucket: "bucket"}, nil)
			wantRequests := int32(1)
			if tc.retryable {
				wantRequests = 2
			}
			if (err == nil) != tc.retryable || requests.Load() != wantRequests {
				t.Fatalf("RETRY_STATUS: status=%d retryable=%t err=%v requests=%d want=%d", tc.status, tc.retryable, err, requests.Load(), wantRequests)
			}
		})
	}
}

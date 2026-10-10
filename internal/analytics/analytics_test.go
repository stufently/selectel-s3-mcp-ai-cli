package analytics

import (
	"context"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBoundedScan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("max-keys") != "2" {
			t.Error("scan did not bound page")
		}
		_, _ = w.Write([]byte("<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>a/x</Key><Size>5</Size></Contents><Contents><Key>b/y</Key><Size>20</Size></Contents></ListBucketResult>"))
	}))
	defer server.Close()
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	c, e := s3core.New(config.Config{Pool: p.DefaultPool, Endpoint: server.URL, AccessKey: "fake", SecretKey: "fake"}, p)
	if e != nil {
		t.Fatal(e)
	}
	out, e := Query(context.Background(), c, "s3_largest_objects", s3core.Args{Bucket: "bucket", MaxScan: 2, Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if out["scanned"] != 2 || out["continuation_token"] != "next" {
		t.Fatal("scan boundary metadata lost")
	}
	objects := out["objects"].([]map[string]any)
	if len(objects) != 1 || *objects[0]["key"].(*string) != "b/y" {
		t.Fatal("largest object selection wrong")
	}
}

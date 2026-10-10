//go:build conformance

package conformance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/analytics"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/mcpserver"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) (context.Context, *s3core.Client) {
	t.Helper()
	p, e := provider.Load()
	if e != nil {
		t.Fatal(e)
	}
	c, e := config.Load(p, config.Overrides{AllowWrite: true, AllowDelete: true, AllowAdmin: true, Policy: "allow"})
	if e != nil {
		t.Fatal(e)
	}
	core, e := s3core.New(c, p)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, e = core.Execute(ctx, "s3_list_buckets", s3core.Args{}, nil)
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway readiness: %v", e)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ctx, core
}
func call(t *testing.T, ctx context.Context, c *s3core.Client, name string, a s3core.Args) map[string]any {
	t.Helper()
	out, e := c.Execute(ctx, name, a, nil)
	if e != nil {
		t.Fatalf("%s: %v", name, e)
	}
	return out.(map[string]any)
}
func bucketName(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }
func TestS3(t *testing.T) {
	ctx, c := setup(t)
	bucket := bucketName("contract")
	call(t, ctx, c, "s3_create_bucket", s3core.Args{Bucket: bucket})
	// Cleanup only the unique test bucket. Each API failure remains visible.
	t.Cleanup(func() {
		out, e := c.SDK.ListObjectVersions(context.Background(), &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)})
		if e == nil {
			for _, v := range out.Versions {
				_, _ = c.SDK.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: v.Key, VersionId: v.VersionId})
			}
			for _, v := range out.DeleteMarkers {
				_, _ = c.SDK.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: v.Key, VersionId: v.VersionId})
			}
		}
		_, _ = c.SDK.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	})
	for i := range 5 {
		call(t, ctx, c, "s3_put_object", s3core.Args{Bucket: bucket, Key: fmt.Sprintf("a/%d", i), Data: strings.Repeat("x", i+1)})
	}
	first := call(t, ctx, c, "s3_list_objects", s3core.Args{Bucket: bucket, MaxKeys: 2})
	if len(first["objects"].([]map[string]any)) != 2 || first["continuation_token"] == "" {
		t.Fatal("pagination first page")
	}
	token := first["continuation_token"].(string)
	count := 2
	for token != "" {
		page := call(t, ctx, c, "s3_list_objects", s3core.Args{Bucket: bucket, MaxKeys: 2, Token: token})
		count += len(page["objects"].([]map[string]any))
		token = page["continuation_token"].(string)
	}
	if count != 5 {
		t.Fatal("pagination did not cover five objects")
	}
	head := call(t, ctx, c, "s3_head_object", s3core.Args{Bucket: bucket, Key: "a/4"})
	if *head["size"].(*int64) != 5 {
		t.Fatal("head size")
	}
	get := call(t, ctx, c, "s3_get_object", s3core.Args{Bucket: bucket, Key: "a/4"})
	if get["data"] != "xxxxx" {
		t.Fatal("get content")
	}
	if _, e := c.Execute(ctx, "s3_put_object", s3core.Args{Bucket: bucket, Key: "a/4", Data: "bad"}, nil); e == nil {
		t.Fatal("default overwrite protection")
	}
	t.Run("conditional-write", func(t *testing.T) {
		_, e := c.SDK.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("a/4"), Body: strings.NewReader("bad"), IfNoneMatch: aws.String("*")})
		if e == nil {
			t.Fatal("gateway silently ignored If-None-Match")
		}
		mapped := s3core.MapError(e)
		if mapped.Code != "PreconditionFailed" {
			t.Fatalf("P1 conditional write gateway response: %v", e)
		}
	})
	signed := call(t, ctx, c, "s3_presign_get", s3core.Args{Bucket: bucket, Key: "a/4"})
	data := signedHTTP(t, ctx, http.MethodGet, signed["url"].(string), nil, nil)
	if string(data) != "xxxxx" {
		t.Fatal("presign GET")
	}
	put := call(t, ctx, c, "s3_presign_put", s3core.Args{Bucket: bucket, Key: "signed"})
	signedHTTP(t, ctx, http.MethodPut, put["url"].(string), []byte("signed-data"), nil)
	upload := call(t, ctx, c, "s3_multipart_create", s3core.Args{Bucket: bucket, Key: "parts"})
	id := *upload["upload_id"].(*string)
	part := call(t, ctx, c, "s3_multipart_presign_part", s3core.Args{Bucket: bucket, Key: "parts", UploadID: id, PartNumber: 1})
	req, e := http.NewRequestWithContext(ctx, http.MethodPut, part["url"].(string), strings.NewReader("part-data"))
	if e != nil {
		t.Fatal(e)
	}
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("part PUT %d %s", response.StatusCode, body)
	}
	call(t, ctx, c, "s3_list_parts", s3core.Args{Bucket: bucket, Key: "parts", UploadID: id})
	call(t, ctx, c, "s3_multipart_complete", s3core.Args{Bucket: bucket, Key: "parts", UploadID: id, Parts: []s3core.Part{{Number: 1, ETag: response.Header.Get("ETag")}}})
	call(t, ctx, c, "s3_copy_object", s3core.Args{Bucket: bucket, Key: "copied", SourceBucket: bucket, SourceKey: "parts"})
	call(t, ctx, c, "s3_move_object", s3core.Args{Bucket: bucket, Key: "moved", SourceBucket: bucket, SourceKey: "copied"})
	if _, e = c.SDK.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String("copied")}); !s3core.NotFound(e) {
		t.Fatal("move left source")
	}
	call(t, ctx, c, "s3_delete_object", s3core.Args{Bucket: bucket, Key: "moved"})
	call(t, ctx, c, "s3_delete_objects", s3core.Args{Bucket: bucket, Objects: []s3core.ObjectID{{Key: "signed"}}})
	largest, e := analytics.Query(ctx, c, "s3_largest_objects", s3core.Args{Bucket: bucket, MaxScan: 100, Limit: 2})
	if e != nil || len(largest["objects"].([]map[string]any)) != 2 {
		t.Fatalf("largest: %v", e)
	}
	usage, e := analytics.Query(ctx, c, "s3_prefix_usage", s3core.Args{Bucket: bucket, MaxScan: 100})
	if e != nil || len(usage["prefixes"].([]map[string]any)) < 1 {
		t.Fatalf("usage: %v", e)
	}
	unfinished := call(t, ctx, c, "s3_multipart_create", s3core.Args{Bucket: bucket, Key: "unfinished"})
	unfinishedID := *unfinished["upload_id"].(*string)
	time.Sleep(10 * time.Millisecond)
	old, e := analytics.Query(ctx, c, "s3_unfinished_uploads", s3core.Args{Bucket: bucket, OlderThanHours: 0.000001})
	if e != nil || len(old["uploads"].([]map[string]any)) != 1 {
		t.Fatalf("unfinished: %v %+v", e, old)
	}
	call(t, ctx, c, "s3_multipart_abort", s3core.Args{Bucket: bucket, Key: "unfinished", UploadID: unfinishedID})
	c.Profile.Features["object_tagging"] = true
	call(t, ctx, c, "s3_put_object_tagging", s3core.Args{Bucket: bucket, Key: "parts", Tags: map[string]string{"purpose": "test"}})
	tags := call(t, ctx, c, "s3_get_object_tagging", s3core.Args{Bucket: bucket, Key: "parts"})
	if len(tags["tags"].([]types.Tag)) != 1 {
		t.Fatal("tags")
	}
	call(t, ctx, c, "s3_put_bucket_cors", s3core.Args{Bucket: bucket, CORSRules: []types.CORSRule{{AllowedMethods: []string{"GET"}, AllowedOrigins: []string{"https://example.test"}}}})
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`, bucket)
	call(t, ctx, c, "s3_put_bucket_policy", s3core.Args{Bucket: bucket, Policy: policy})
	cfg := call(t, ctx, c, "s3_get_bucket_config", s3core.Args{Bucket: bucket})
	if cfg["cors"] == nil || cfg["bucket_policy"] == nil {
		t.Fatal("bucket config")
	}
	call(t, ctx, c, "s3_delete_bucket_policy", s3core.Args{Bucket: bucket})
	call(t, ctx, c, "s3_put_bucket_versioning", s3core.Args{Bucket: bucket, Status: "Enabled"})
	v1 := call(t, ctx, c, "s3_put_object", s3core.Args{Bucket: bucket, Key: "versioned", Data: "first"})
	call(t, ctx, c, "s3_put_object", s3core.Args{Bucket: bucket, Key: "versioned", Data: "second", Overwrite: true})
	versions := call(t, ctx, c, "s3_list_object_versions", s3core.Args{Bucket: bucket, Prefix: "versioned"})
	if len(versions["versions"].([]types.ObjectVersion)) != 2 {
		t.Fatal("two versions not returned")
	}
	call(t, ctx, c, "s3_delete_object", s3core.Args{Bucket: bucket, Key: "versioned", VersionID: *v1["version_id"].(*string)})
	lockBucket := bucketName("locked")
	call(t, ctx, c, "s3_create_bucket", s3core.Args{Bucket: lockBucket, ObjectLock: true})
	lock := call(t, ctx, c, "s3_get_bucket_config", s3core.Args{Bucket: lockBucket})
	if lock["object_lock_config"].(*types.ObjectLockConfiguration).ObjectLockEnabled != types.ObjectLockEnabledEnabled {
		t.Fatal("Object Lock not enabled")
	}
	call(t, ctx, c, "s3_delete_bucket", s3core.Args{Bucket: lockBucket})
	empty := bucketName("empty")
	call(t, ctx, c, "s3_create_bucket", s3core.Args{Bucket: empty})
	call(t, ctx, c, "s3_delete_bucket", s3core.Args{Bucket: empty})
}
func signedHTTP(t *testing.T, ctx context.Context, method, url string, data []byte, headers http.Header) []byte {
	t.Helper()
	req, e := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	if headers != nil {
		req.Header = headers
	}
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = response.Body.Close() }()
	body, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("signed HTTP %d %s", response.StatusCode, body)
	}
	return body
}
func TestMCP(t *testing.T) {
	ctx, c := setup(t)
	bucket := bucketName("mcp")
	call(t, ctx, c, "s3_create_bucket", s3core.Args{Bucket: bucket})
	call(t, ctx, c, "s3_put_object", s3core.Args{Bucket: bucket, Key: "backups/file", Data: "protected"})
	t.Cleanup(func() {
		_, _ = c.SDK.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("backups/file")})
		_, _ = c.SDK.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	})
	root := t.TempDir()
	outside := t.TempDir()
	if e := os.Symlink(outside, filepath.Join(root, "escape")); e != nil {
		t.Fatal(e)
	}
	cfg := c.Config
	cfg.Policy = "confirm"
	cfg.FileRoot = root
	cfg.Prefixes = []string{bucket + "/backups/"}
	cfg.AuditFile = filepath.Join(t.TempDir(), "audit.jsonl")
	core, e := s3core.New(cfg, c.Profile)
	if e != nil {
		t.Fatal(e)
	}
	server, e := mcpserver.New(c.Profile, cfg, core, "test")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = server.Close() }()
	ct, st := mcp.NewInMemoryTransports()
	ss, e := server.MCP.Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = ss.Close() }()
	client, e := mcp.NewClient(&mcp.Implementation{Name: "conformance", Version: "test"}, nil).Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = client.Close() }()
	tools, e := client.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(tools.Tools) != 33 {
		t.Fatalf("file root tool count: %d", len(tools.Tools))
	}
	invoke := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		out, e := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	if !invoke("s3_delete_object", map[string]any{"bucket": bucket, "key": "backups/file"}).IsError {
		t.Fatal("unconfirmed delete accepted")
	}
	if got := call(t, ctx, c, "s3_get_object", s3core.Args{Bucket: bucket, Key: "backups/file"}); got["data"] != "protected" {
		t.Fatal("gate changed object")
	}
	if !invoke("s3_list_objects", map[string]any{"bucket": bucket}).IsError {
		t.Fatal("broad scoped list accepted")
	}
	if !invoke("s3_presign_get", map[string]any{"bucket": bucket, "key": "backups-evil/file"}).IsError {
		t.Fatal("scope escape")
	}
	if invoke("s3_download_file", map[string]any{"bucket": bucket, "key": "backups/file", "path": "download"}).IsError {
		t.Fatal("in-root download failed")
	}
	if !invoke("s3_download_file", map[string]any{"bucket": bucket, "key": "backups/file", "path": "escape/file"}).IsError {
		t.Fatal("out-of-root download accepted")
	}
	if !invoke("s3_upload_file", map[string]any{"bucket": bucket, "key": "backups/new", "path": "escape/file"}).IsError {
		t.Fatal("out-of-root upload accepted")
	}
	if invoke("s3_upload_file", map[string]any{"bucket": bucket, "key": "backups/uploaded", "path": "download"}).IsError {
		t.Fatal("in-root upload failed")
	}
	call(t, ctx, c, "s3_delete_object", s3core.Args{Bucket: bucket, Key: "backups/uploaded"})
	if invoke("s3_presign_get", map[string]any{"bucket": bucket, "key": "backups/file"}).IsError {
		t.Fatal("scoped presign failed")
	}
	audit, e := os.ReadFile(cfg.AuditFile)
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(string(audit)), "\n")
	if len(lines) != 8 {
		t.Fatalf("audit count %d for eight MCP calls", len(lines))
	}
	for _, line := range lines {
		var record map[string]any
		if e = json.Unmarshal([]byte(line), &record); e != nil {
			t.Fatal(e)
		}
		for _, key := range []string{"ts", "tool", "s3_action", "bucket", "key", "outcome", "gated", "duration_ms"} {
			if _, ok := record[key]; !ok {
				t.Fatal("audit field missing")
			}
		}
	}
	if strings.Contains(string(audit), cfg.SecretKey) || strings.Contains(string(audit), "X-Amz-Signature") {
		t.Fatal("audit leaked credentials or signature")
	}
}
func TestCLI(t *testing.T) {
	ctx, c := setup(t)
	binary := filepath.Join(t.TempDir(), "cli")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/"+c.Profile.Binary) //nolint:gosec // Embedded profile selects the build target; output is beneath t.TempDir.
	build.Dir = "/src"
	if data, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, data)
	}
	bucket := bucketName("cli")
	home := t.TempDir()
	run := func(expected int, args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // Runs only the fixture binary built above, without a shell.
		cmd.Env = append(os.Environ(), "HOME="+home)
		data, e := cmd.CombinedOutput()
		rc := 0
		if e != nil {
			var exit *exec.ExitError
			var ok bool
			exit, ok = e.(*exec.ExitError)
			if !ok {
				t.Fatal(e)
			}
			rc = exit.ExitCode()
		}
		if rc != expected {
			t.Fatalf("CLI %v rc=%d expected=%d: %s", args, rc, expected, data)
		}
		return data
	}
	run(0, "mb", bucket)
	source := filepath.Join(t.TempDir(), "large")
	data := bytes.Repeat([]byte("abcdefgh"), 20*1024*1024/8)
	if e := os.WriteFile(source, data, 0600); e != nil {
		t.Fatal(e)
	}
	run(0, "cp", "--part-size", "5242880", source, "s3://"+bucket+"/large")
	dest := filepath.Join(t.TempDir(), "download")
	run(0, "cp", "s3://"+bucket+"/large", dest)
	download, e := os.ReadFile(dest)
	if e != nil || sha256.Sum256(data) != sha256.Sum256(download) {
		t.Fatal("20 MiB roundtrip checksum")
	}
	listing := run(0, "ls", bucket, "--json")
	var parsed map[string]any
	if e = json.Unmarshal(listing, &parsed); e != nil {
		t.Fatal(e)
	}
	signed := strings.TrimSpace(string(run(0, "presign", "s3://"+bucket+"/large")))
	if received := signedHTTP(t, ctx, http.MethodGet, signed, nil, nil); sha256.Sum256(received) != sha256.Sum256(data) {
		t.Fatal("CLI presign GET")
	}
	run(2, "rm", "s3://"+bucket+"/large")
	run(0, "rm", "--yes", "s3://"+bucket+"/large")
	run(0, "rb", "--yes", bucket)
}

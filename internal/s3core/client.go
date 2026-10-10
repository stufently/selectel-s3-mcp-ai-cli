// Package s3core implements provider-neutral, guarded S3 operations.
package s3core

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/safety"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const InlineLimit int64 = 262144

type Part struct {
	Number int32  `json:"part_number"`
	ETag   string `json:"etag"`
}
type ObjectID struct {
	Key       string `json:"key"`
	VersionID string `json:"version_id,omitempty"`
}
type Args struct {
	Offset            int                            `json:"result_offset,omitempty"`
	Bucket            string                         `json:"bucket,omitempty"`
	Key               string                         `json:"key,omitempty"`
	Prefix            string                         `json:"prefix,omitempty"`
	Token             string                         `json:"continuation_token,omitempty"`
	MaxKeys           int32                          `json:"max_keys,omitempty"`
	VersionID         string                         `json:"version_id,omitempty"`
	VersionMarker     string                         `json:"version_marker,omitempty"`
	Data              string                         `json:"data,omitempty"`
	Encoding          string                         `json:"encoding,omitempty"`
	ContentType       string                         `json:"content_type,omitempty"`
	MaxBytes          int64                          `json:"max_bytes,omitempty"`
	Expires           int64                          `json:"expires_seconds,omitempty"`
	SourceBucket      string                         `json:"source_bucket,omitempty"`
	SourceKey         string                         `json:"source_key,omitempty"`
	Overwrite         bool                           `json:"overwrite,omitempty"`
	Confirm           bool                           `json:"confirm,omitempty"`
	Objects           []ObjectID                     `json:"objects,omitempty"`
	UploadID          string                         `json:"upload_id,omitempty"`
	PartNumber        int32                          `json:"part_number,omitempty"`
	Parts             []Part                         `json:"parts,omitempty"`
	PartSize          int64                          `json:"part_size,omitempty"`
	Path              string                         `json:"path,omitempty"`
	Status            string                         `json:"status,omitempty"`
	Policy            string                         `json:"policy,omitempty"`
	CORSRules         []types.CORSRule               `json:"cors_rules,omitempty"`
	LockConfiguration *types.ObjectLockConfiguration `json:"lock_configuration,omitempty"`
	ObjectLock        bool                           `json:"object_lock,omitempty"`
	Tags              map[string]string              `json:"tags,omitempty"`
	MaxScan           int                            `json:"max_scan,omitempty"`
	Limit             int                            `json:"limit,omitempty"`
	OlderThanHours    float64                        `json:"older_than_hours,omitempty"`
}
type Client struct {
	SDK     *s3.Client
	Profile provider.Profile
	Config  config.Config
	scope   safety.Scope
}

func New(c config.Config, p provider.Profile) (*Client, error) {
	cfg, e := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(c.Pool), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")), awsconfig.WithRetryer(func() aws.Retryer {
		return retry.NewAdaptiveMode(func(o *retry.AdaptiveModeOptions) {
			o.StandardOptions = append(o.StandardOptions, func(s *retry.StandardOptions) {
				s.Retryables = append(s.Retryables, retry.RetryableHTTPStatusCode{Codes: map[int]struct{}{http.StatusTooManyRequests: {}}})
			})
		})
	}))
	if e != nil {
		return nil, e
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(c.Endpoint)
		o.UsePathStyle = p.PathStyle
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	out := &Client{SDK: client, Profile: p, Config: c}
	return out, out.SetScope()
}
func (c *Client) SetScope() error {
	s, e := safety.NewScope(c.Config.Buckets, c.Config.Prefixes)
	c.scope = s
	return e
}
func (c *Client) Scope() safety.Scope { return c.scope }
func Gated(name string, a Args) bool {
	switch name {
	case "s3_delete_object", "s3_delete_objects", "s3_move_object", "s3_multipart_abort", "s3_delete_bucket", "s3_put_bucket_policy", "s3_delete_bucket_policy", "s3_put_object_lock_configuration", "s3_presign_put":
		return true
	case "s3_put_bucket_versioning":
		return a.Status == "Suspended"
	case "s3_put_object", "s3_copy_object", "s3_multipart_complete", "s3_upload_file", "s3_download_file":
		return a.Overwrite
	}
	return false
}
func wholeBucket(name string) bool {
	return name == "s3_get_bucket_config" || name == "s3_create_bucket" || name == "s3_delete_bucket" || strings.HasPrefix(name, "s3_put_bucket_") || name == "s3_delete_bucket_policy" || name == "s3_put_object_lock_configuration"
}
func listing(name string) bool {
	switch name {
	case "s3_list_objects", "s3_list_object_versions", "s3_list_multipart_uploads", "s3_largest_objects", "s3_prefix_usage", "s3_unfinished_uploads":
		return true
	}
	return false
}
func (c *Client) Guard(ctx context.Context, name string, a Args, ask safety.Elicitor) error {
	if name != "s3_list_buckets" && name != "s3_delete_objects" {
		key := a.Key
		if listing(name) {
			key = a.Prefix
		}
		if e := c.scope.Check(a.Bucket, key, wholeBucket(name)); e != nil {
			return e
		}
		if listing(name) {
			for _, p := range c.scope.Prefixes[a.Bucket] {
				if key == p {
					return fmt.Errorf("scope_denied: listing requires prefix %s/", p)
				}
			}
		}
	}
	if name == "s3_copy_object" || name == "s3_move_object" {
		if a.SourceKey == "" || a.Key == "" {
			return fmt.Errorf("source_key and key are required")
		}
		if e := c.scope.Check(a.SourceBucket, a.SourceKey, false); e != nil {
			return e
		}
		if a.Bucket == a.SourceBucket && a.Key == a.SourceKey {
			return fmt.Errorf("source and destination must differ")
		}
	}
	if name == "s3_delete_objects" {
		if len(a.Objects) == 0 || len(a.Objects) > 1000 {
			return fmt.Errorf("objects must contain 1..1000 entries")
		}
		for _, o := range a.Objects {
			if o.Key == "" {
				return fmt.Errorf("object key is required")
			}
			if e := c.scope.Check(a.Bucket, o.Key, false); e != nil {
				return e
			}
		}
	}
	if Gated(name, a) {
		return (safety.Gate{Policy: c.Config.Policy}).Check(ctx, a.Confirm, ask, fmt.Sprintf("%s on %s/%s; this may overwrite or permanently remove data/configuration", name, a.Bucket, a.Key))
	}
	return nil
}
func (c *Client) ensureAbsent(ctx context.Context, a Args) error {
	if a.Overwrite {
		return nil
	}
	_, e := c.SDK.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(a.Bucket), Key: aws.String(a.Key)})
	if e == nil {
		return &Error{Code: "already_exists", Message: "destination exists; overwrite:true requires destructive approval"}
	}
	if !NotFound(e) {
		return e
	}
	return nil
}
func (c *Client) condition(a Args) *string {
	if !a.Overwrite && c.Profile.Features["conditional_writes"] {
		return aws.String("*")
	}
	return nil
}
func page(n int32) (int32, error) {
	if n == 0 {
		return 1000, nil
	}
	if n < 1 || n > 1000 {
		return 0, fmt.Errorf("max_keys must be 1..1000")
	}
	return n, nil
}
func (c *Client) expiry(n int64) (time.Duration, error) {
	if n == 0 {
		n = 3600
	}
	if n < 1 || n > c.Profile.PresignMaxSeconds {
		return 0, fmt.Errorf("expires_seconds must be 1..%d", c.Profile.PresignMaxSeconds)
	}
	return time.Duration(n) * time.Second, nil
}
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return aws.String(s)
}
func (c *Client) Execute(ctx context.Context, name string, a Args, ask safety.Elicitor) (any, error) {
	if e := c.Guard(ctx, name, a, ask); e != nil {
		return nil, e
	}
	return c.execute(ctx, name, a)
}

// UploadReader serves explicit CLI transfers; MCP supplies readers confined by os.Root.
func (c *Client) UploadReader(ctx context.Context, a Args, r io.ReadSeeker, ask safety.Elicitor) (any, error) {
	if e := c.Guard(ctx, "s3_upload_file", a, ask); e != nil {
		return nil, e
	}
	if e := c.ensureAbsent(ctx, a); e != nil {
		return nil, e
	}
	return c.upload(ctx, a, r)
}
func (c *Client) DownloadWriter(ctx context.Context, a Args, w io.Writer) error {
	if e := c.Guard(ctx, "s3_get_object", a, nil); e != nil {
		return e
	}
	out, e := c.SDK.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.Bucket), Key: aws.String(a.Key), VersionId: optional(a.VersionID)})
	if e != nil {
		return e
	}
	defer func() { _ = out.Body.Close() }()
	_, e = io.Copy(w, out.Body)
	return e
}
func (c *Client) downloadFile(ctx context.Context, a Args) (any, error) {
	files, e := safety.OpenFiles(c.Config.FileRoot)
	if e != nil {
		return nil, e
	}
	defer func() { _ = files.Close() }()
	out, e := c.SDK.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.Bucket), Key: aws.String(a.Key), VersionId: optional(a.VersionID)})
	if e != nil {
		return nil, e
	}
	defer func() { _ = out.Body.Close() }()
	if a.Overwrite {
		e = files.Replace(a.Path, out.Body)
	} else {
		e = files.Write(a.Path, out.Body)
	}
	return map[string]any{"path": a.Path}, e
}
func (c *Client) uploadFile(ctx context.Context, a Args) (any, error) {
	files, e := safety.OpenFiles(c.Config.FileRoot)
	if e != nil {
		return nil, e
	}
	defer func() { _ = files.Close() }()
	in, e := files.Read(a.Path)
	if e != nil {
		return nil, e
	}
	defer func() { _ = in.Close() }()
	if e = c.ensureAbsent(ctx, a); e != nil {
		return nil, e
	}
	return c.upload(ctx, a, in)
}

// EndpointDate uses an unsigned, read-only endpoint request for clock diagnostics.
func EndpointDate(ctx context.Context, endpoint string) (time.Time, error) {
	request, e := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if e != nil {
		return time.Time{}, e
	}
	response, e := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if e != nil {
		return time.Time{}, e
	}
	defer func() { _ = response.Body.Close() }()
	return http.ParseTime(response.Header.Get("Date"))
}

var _ io.ReadSeeker = (*os.File)(nil)

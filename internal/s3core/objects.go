package s3core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

func (c *Client) execute(ctx context.Context, name string, a Args) (any, error) {
	b, k := aws.String(a.Bucket), aws.String(a.Key)
	switch name {
	case "s3_list_buckets":
		n, e := page(a.MaxKeys)
		if e != nil {
			return nil, e
		}
		out, e := c.SDK.ListBuckets(ctx, &s3.ListBucketsInput{MaxBuckets: aws.Int32(n), ContinuationToken: optional(a.Token)})
		if e != nil {
			return nil, e
		}
		buckets := []map[string]any{}
		for _, v := range out.Buckets {
			if c.scope.BucketVisible(aws.ToString(v.Name)) {
				buckets = append(buckets, map[string]any{"name": v.Name, "created_at": v.CreationDate})
			}
		}
		return map[string]any{"buckets": buckets, "continuation_token": out.ContinuationToken}, nil
	case "s3_list_objects":
		n, e := page(a.MaxKeys)
		if e != nil {
			return nil, e
		}
		out, e := c.SDK.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: b, Prefix: optional(a.Prefix), MaxKeys: aws.Int32(n), ContinuationToken: optional(a.Token)})
		if e != nil {
			return nil, e
		}
		objects := []map[string]any{}
		for _, v := range out.Contents {
			objects = append(objects, map[string]any{"key": v.Key, "size": aws.ToInt64(v.Size), "etag": v.ETag, "last_modified": v.LastModified, "storage_class": v.StorageClass})
		}
		return map[string]any{"objects": objects, "continuation_token": aws.ToString(out.NextContinuationToken), "truncated": aws.ToBool(out.IsTruncated)}, nil
	case "s3_head_object":
		out, e := c.SDK.HeadObject(ctx, &s3.HeadObjectInput{Bucket: b, Key: k, VersionId: optional(a.VersionID)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"key": a.Key, "size": out.ContentLength, "etag": out.ETag, "content_type": out.ContentType, "last_modified": out.LastModified, "version_id": out.VersionId, "metadata": out.Metadata}, nil
	case "s3_get_object", "s3_preview_object":
		return c.get(ctx, name, a)
	case "s3_put_object", "s3_create_folder":
		if a.Key == "" {
			return nil, fmt.Errorf("key is required")
		}
		if name == "s3_create_folder" {
			a.Key = strings.TrimSuffix(a.Key, "/") + "/"
			a.Data = ""
			a.Overwrite = false
			k = aws.String(a.Key)
		}
		data := []byte(a.Data)
		if a.Encoding == "base64" {
			var e error
			data, e = base64.StdEncoding.DecodeString(a.Data)
			if e != nil {
				return nil, fmt.Errorf("invalid base64 data")
			}
		} else if a.Encoding != "" && a.Encoding != "text" {
			return nil, fmt.Errorf("encoding must be text or base64")
		}
		maxBytes := a.MaxBytes
		if maxBytes == 0 {
			maxBytes = InlineLimit
		}
		if maxBytes < 1 || maxBytes > InlineLimit || int64(len(data)) > maxBytes {
			return nil, fmt.Errorf("inline put exceeds max_bytes (ceiling 262144); use s3_presign_put")
		}
		if e := c.ensureAbsent(ctx, a); e != nil {
			return nil, e
		}
		out, e := c.SDK.PutObject(ctx, &s3.PutObjectInput{Bucket: b, Key: k, Body: bytes.NewReader(data), ContentType: optional(a.ContentType), IfNoneMatch: c.condition(a)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"etag": out.ETag, "version_id": out.VersionId}, nil
	case "s3_copy_object", "s3_move_object":
		if e := c.ensureAbsent(ctx, a); e != nil {
			return nil, e
		}
		source := url.PathEscape(a.SourceBucket + "/" + a.SourceKey)
		out, e := c.SDK.CopyObject(ctx, &s3.CopyObjectInput{Bucket: b, Key: k, CopySource: aws.String(source), IfNoneMatch: c.condition(a)})
		if e != nil {
			return nil, e
		}
		if name == "s3_move_object" {
			if _, e = c.SDK.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(a.SourceBucket), Key: aws.String(a.SourceKey)}); e != nil {
				return nil, fmt.Errorf("copy succeeded but source deletion failed: %w", e)
			}
		}
		return map[string]any{"result": out.CopyObjectResult, "version_id": out.VersionId}, nil
	case "s3_delete_object":
		out, e := c.SDK.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: b, Key: k, VersionId: optional(a.VersionID)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"delete_marker": out.DeleteMarker, "version_id": out.VersionId}, nil
	case "s3_delete_objects":
		objects := make([]types.ObjectIdentifier, 0, len(a.Objects))
		for _, o := range a.Objects {
			objects = append(objects, types.ObjectIdentifier{Key: aws.String(o.Key), VersionId: optional(o.VersionID)})
		}
		out, e := c.SDK.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: b, Delete: &types.Delete{Objects: objects}})
		if e != nil {
			return nil, e
		}
		return map[string]any{"deleted": out.Deleted, "errors": out.Errors}, nil
	case "s3_presign_get", "s3_presign_put":
		expiry, e := c.expiry(a.Expires)
		if e != nil {
			return nil, e
		}
		presign := s3.NewPresignClient(c.SDK, func(o *s3.PresignOptions) { o.Expires = expiry })
		if name == "s3_presign_get" {
			out, e := presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: b, Key: k, VersionId: optional(a.VersionID)})
			if e != nil {
				return nil, e
			}
			return map[string]any{"url": out.URL, "method": out.Method, "headers": out.SignedHeader, "expires_seconds": int64(expiry.Seconds())}, nil
		}
		out, e := presign.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: b, Key: k, ContentType: optional(a.ContentType)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"url": out.URL, "method": out.Method, "headers": out.SignedHeader, "expires_seconds": int64(expiry.Seconds())}, nil
	case "s3_get_object_tagging":
		out, e := c.SDK.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{Bucket: b, Key: k, VersionId: optional(a.VersionID)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"tags": out.TagSet}, nil
	case "s3_put_object_tagging":
		tags := []types.Tag{}
		for key, value := range a.Tags {
			tags = append(tags, types.Tag{Key: aws.String(key), Value: aws.String(value)})
		}
		_, e := c.SDK.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{Bucket: b, Key: k, VersionId: optional(a.VersionID), Tagging: &types.Tagging{TagSet: tags}})
		return map[string]any{"updated": e == nil}, e
	case "s3_download_file":
		return c.downloadFile(ctx, a)
	case "s3_upload_file":
		return c.uploadFile(ctx, a)
	case "s3_multipart_create", "s3_multipart_presign_part", "s3_multipart_complete", "s3_multipart_abort", "s3_list_parts", "s3_list_multipart_uploads":
		return c.multipart(ctx, name, a)
	default:
		return c.bucket(ctx, name, a)
	}
}
func (c *Client) get(ctx context.Context, name string, a Args) (any, error) {
	max := a.MaxBytes
	if max == 0 {
		max = InlineLimit
	}
	if name == "s3_preview_object" && a.MaxBytes == 0 {
		max = 65536
	}
	ceiling := InlineLimit
	if name == "s3_preview_object" {
		ceiling = 65536
	}
	if max < 1 || max > ceiling {
		return nil, fmt.Errorf("max_bytes must be 1..%d; use s3_presign_get for larger objects", ceiling)
	}
	input := &s3.GetObjectInput{Bucket: aws.String(a.Bucket), Key: aws.String(a.Key), VersionId: optional(a.VersionID)}
	if name == "s3_preview_object" {
		input.Range = aws.String(fmt.Sprintf("bytes=0-%d", max-1))
	}
	out, e := c.SDK.GetObject(ctx, input)
	if e != nil {
		return nil, e
	}
	defer func() { _ = out.Body.Close() }()
	data, e := io.ReadAll(io.LimitReader(out.Body, max+1))
	if e != nil {
		return nil, e
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("object exceeds max_bytes; use s3_presign_get")
	}
	encoding := a.Encoding
	if encoding == "" {
		encoding = "text"
		if name == "s3_preview_object" && !utf8.Valid(data) {
			encoding = "base64"
		}
	}
	text := string(data)
	if encoding == "base64" {
		text = base64.StdEncoding.EncodeToString(data)
	} else if encoding != "text" || !utf8.Valid(data) {
		return nil, fmt.Errorf("object is not UTF-8 text; use encoding:base64 or s3_presign_get")
	}
	result := map[string]any{"key": a.Key, "bytes": len(data), "data": text, "encoding": encoding, "content_type": out.ContentType}
	if name == "s3_preview_object" {
		result["detected_type"] = http.DetectContentType(data)
		if strings.Contains(strings.ToLower(aws.ToString(out.ContentType)), "csv") || strings.HasSuffix(strings.ToLower(a.Key), ".csv") {
			header, e := csv.NewReader(bytes.NewReader(data)).Read()
			if e == nil {
				result["csv_header"] = header
			}
		}
	}
	return result, nil
}

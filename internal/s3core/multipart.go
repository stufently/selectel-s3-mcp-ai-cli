package s3core

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"io"
)

func (c *Client) upload(ctx context.Context, a Args, r io.ReadSeeker) (any, error) {
	if a.PartSize != 0 && a.PartSize < 5*1024*1024 {
		return nil, fmt.Errorf("part_size must be at least 5 MiB")
	}
	uploader := manager.NewUploader(c.SDK, func(u *manager.Uploader) { //nolint:staticcheck // v1 explicitly requires the pinned feature/s3/manager uploader.
		if a.PartSize != 0 {
			u.PartSize = a.PartSize
		}
		u.Concurrency = 2
		u.MaxUploadParts = c.Profile.Limits.MaxParts
	})
	out, e := uploader.Upload(ctx, &s3.PutObjectInput{Bucket: aws.String(a.Bucket), Key: aws.String(a.Key), Body: r, ContentType: optional(a.ContentType), IfNoneMatch: c.condition(a)}) //nolint:staticcheck // v1 requires manager; retain its conditional-write propagation.
	if e != nil {
		return nil, e
	}
	return map[string]any{"etag": out.ETag, "version_id": out.VersionID, "upload_id": out.UploadID}, nil
}
func (c *Client) multipart(ctx context.Context, name string, a Args) (any, error) {
	b, k := aws.String(a.Bucket), aws.String(a.Key)
	switch name {
	case "s3_multipart_create":
		out, e := c.SDK.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: b, Key: k, ContentType: optional(a.ContentType)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"upload_id": out.UploadId}, nil
	case "s3_multipart_presign_part":
		if a.PartNumber < 1 || a.PartNumber > c.Profile.Limits.MaxParts || a.UploadID == "" {
			return nil, fmt.Errorf("upload_id and part_number in 1..%d required", c.Profile.Limits.MaxParts)
		}
		expiry, e := c.expiry(a.Expires)
		if e != nil {
			return nil, e
		}
		out, e := s3.NewPresignClient(c.SDK, func(o *s3.PresignOptions) { o.Expires = expiry }).PresignUploadPart(ctx, &s3.UploadPartInput{Bucket: b, Key: k, UploadId: aws.String(a.UploadID), PartNumber: aws.Int32(a.PartNumber)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"url": out.URL, "method": out.Method, "headers": out.SignedHeader}, nil
	case "s3_multipart_complete":
		if a.UploadID == "" || len(a.Parts) == 0 || len(a.Parts) > int(c.Profile.Limits.MaxParts) {
			return nil, fmt.Errorf("upload_id and nonempty parts required")
		}
		if e := c.ensureAbsent(ctx, a); e != nil {
			return nil, e
		}
		parts := []types.CompletedPart{}
		var previous int32
		for _, part := range a.Parts {
			if part.Number <= previous || part.Number > c.Profile.Limits.MaxParts || part.ETag == "" {
				return nil, fmt.Errorf("parts must have ascending numbers and etags")
			}
			previous = part.Number
			parts = append(parts, types.CompletedPart{PartNumber: aws.Int32(part.Number), ETag: aws.String(part.ETag)})
		}
		out, e := c.SDK.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: b, Key: k, UploadId: aws.String(a.UploadID), MultipartUpload: &types.CompletedMultipartUpload{Parts: parts}, IfNoneMatch: c.condition(a)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"etag": out.ETag, "version_id": out.VersionId}, nil
	case "s3_multipart_abort":
		_, e := c.SDK.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: b, Key: k, UploadId: aws.String(a.UploadID)})
		return map[string]any{"aborted": e == nil}, e
	case "s3_list_parts":
		n, e := page(a.MaxKeys)
		if e != nil {
			return nil, e
		}
		out, e := c.SDK.ListParts(ctx, &s3.ListPartsInput{Bucket: b, Key: k, UploadId: aws.String(a.UploadID), MaxParts: aws.Int32(n), PartNumberMarker: optional(a.Token)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"parts": out.Parts, "continuation_token": out.NextPartNumberMarker, "truncated": out.IsTruncated}, nil
	case "s3_list_multipart_uploads":
		n, e := page(a.MaxKeys)
		if e != nil {
			return nil, e
		}
		out, e := c.SDK.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: b, Prefix: optional(a.Prefix), MaxUploads: aws.Int32(n), KeyMarker: optional(a.Token), UploadIdMarker: optional(a.UploadID)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"uploads": out.Uploads, "continuation_token": out.NextKeyMarker, "upload_id_marker": out.NextUploadIdMarker, "truncated": out.IsTruncated}, nil
	}
	return nil, fmt.Errorf("unknown multipart operation")
}

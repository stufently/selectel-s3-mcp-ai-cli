package s3core

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func (c *Client) bucket(ctx context.Context, name string, a Args) (any, error) {
	b := aws.String(a.Bucket)
	switch name {
	case "s3_create_bucket":
		input := &s3.CreateBucketInput{Bucket: b, ObjectLockEnabledForBucket: aws.Bool(a.ObjectLock)}
		if c.Config.Pool != "us-east-1" {
			input.CreateBucketConfiguration = &types.CreateBucketConfiguration{LocationConstraint: types.BucketLocationConstraint(c.Config.Pool)}
		}
		out, e := c.SDK.CreateBucket(ctx, input)
		if e != nil {
			return nil, e
		}
		return map[string]any{"location": out.Location}, nil
	case "s3_delete_bucket":
		_, e := c.SDK.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: b})
		return map[string]any{"deleted": e == nil}, e
	case "s3_put_bucket_versioning":
		if a.Status != "Enabled" && a.Status != "Suspended" {
			return nil, fmt.Errorf("status must be Enabled or Suspended")
		}
		_, e := c.SDK.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: b, VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatus(a.Status)}})
		return map[string]any{"status": a.Status}, e
	case "s3_put_bucket_cors":
		_, e := c.SDK.PutBucketCors(ctx, &s3.PutBucketCorsInput{Bucket: b, CORSConfiguration: &types.CORSConfiguration{CORSRules: a.CORSRules}})
		return map[string]any{"updated": e == nil}, e
	case "s3_put_bucket_policy":
		if a.Policy == "" {
			return nil, fmt.Errorf("policy required")
		}
		_, e := c.SDK.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: b, Policy: aws.String(a.Policy)})
		return map[string]any{"updated": e == nil}, e
	case "s3_delete_bucket_policy":
		_, e := c.SDK.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: b})
		return map[string]any{"deleted": e == nil}, e
	case "s3_put_object_lock_configuration":
		if a.LockConfiguration == nil {
			return nil, fmt.Errorf("lock_configuration required")
		}
		_, e := c.SDK.PutObjectLockConfiguration(ctx, &s3.PutObjectLockConfigurationInput{Bucket: b, ObjectLockConfiguration: a.LockConfiguration})
		return map[string]any{"updated": e == nil}, e
	case "s3_list_object_versions":
		n, e := page(a.MaxKeys)
		if e != nil {
			return nil, e
		}
		out, e := c.SDK.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: b, Prefix: optional(a.Prefix), MaxKeys: aws.Int32(n), KeyMarker: optional(a.Token), VersionIdMarker: optional(a.VersionMarker)})
		if e != nil {
			return nil, e
		}
		return map[string]any{"versions": out.Versions, "delete_markers": out.DeleteMarkers, "continuation_token": out.NextKeyMarker, "version_marker": out.NextVersionIdMarker, "truncated": out.IsTruncated}, nil
	case "s3_get_bucket_config":
		return c.bucketConfig(ctx, a.Bucket)
	}
	return nil, fmt.Errorf("unknown S3 operation %q", name)
}
func configState(v any, e error) (any, error) {
	if e == nil {
		return v, nil
	}
	var api smithy.APIError
	if errors.As(e, &api) {
		switch api.ErrorCode() {
		case "NoSuchCORSConfiguration", "NoSuchBucketPolicy", "ObjectLockConfigurationNotFoundError", "NoSuchObjectLockConfiguration":
			return map[string]any{"status": "absent"}, nil
		case "NotImplemented", "UnsupportedOperation":
			return map[string]any{"status": "unsupported"}, nil
		}
	}
	return nil, e
}
func (c *Client) bucketConfig(ctx context.Context, bucket string) (any, error) {
	b := aws.String(bucket)
	out := map[string]any{}
	location, e := c.SDK.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: b})
	if e != nil {
		return nil, e
	}
	out["location"] = location.LocationConstraint
	for _, feature := range []string{"versioning", "cors", "bucket_policy", "object_lock_config"} {
		if !c.Profile.Features[feature] {
			out[feature] = map[string]any{"status": "unsupported"}
			continue
		}
		var value any
		switch feature {
		case "versioning":
			v, err := c.SDK.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: b})
			e = err
			if v != nil {
				value = map[string]any{"status": v.Status}
			}
		case "cors":
			v, err := c.SDK.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: b})
			e = err
			if v != nil {
				value = v.CORSRules
			}
		case "bucket_policy":
			v, err := c.SDK.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: b})
			e = err
			if v != nil {
				value = v.Policy
			}
		case "object_lock_config":
			v, err := c.SDK.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{Bucket: b})
			e = err
			if v != nil {
				value = v.ObjectLockConfiguration
			}
		}
		value, e = configState(value, e)
		if e != nil {
			return nil, e
		}
		out[feature] = value
	}
	return out, nil
}

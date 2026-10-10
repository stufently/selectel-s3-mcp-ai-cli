// Package analytics provides explicitly bounded, paginated storage scans.
package analytics

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/s3core"
	"sort"
	"strings"
	"time"
)

func Query(ctx context.Context, c *s3core.Client, name string, a s3core.Args) (map[string]any, error) {
	if e := c.Guard(ctx, name, a, nil); e != nil {
		return nil, e
	}
	max := a.MaxScan
	if max == 0 {
		max = 10000
	}
	if max < 1 {
		return nil, fmt.Errorf("max_scan must be positive")
	}
	limit := a.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("limit must be 1..1000")
	}
	if name == "s3_unfinished_uploads" {
		return unfinished(ctx, c, a, max)
	}
	objects := []map[string]any{}
	token := a.Token
	scanned := 0
	for scanned < max {
		batch := min(1000, max-scanned)
		pageArgs := a
		pageArgs.Token = token
		pageArgs.MaxKeys = int32(batch)
		result, e := c.Execute(ctx, "s3_list_objects", pageArgs, nil)
		if e != nil {
			return nil, e
		}
		page := result.(map[string]any)
		items := page["objects"].([]map[string]any)
		objects = append(objects, items...)
		scanned += len(items)
		next := page["continuation_token"].(string)
		if next == "" {
			token = ""
			break
		}
		if next == token {
			return nil, fmt.Errorf("S3 repeated continuation token")
		}
		token = next
	}
	result := map[string]any{"scanned": scanned, "continuation_token": token, "partial": token != ""}
	if name == "s3_largest_objects" {
		sort.Slice(objects, func(i, j int) bool { return objects[i]["size"].(int64) > objects[j]["size"].(int64) })
		result["objects"] = objects[:min(limit, len(objects))]
		return result, nil
	}
	if name != "s3_prefix_usage" {
		return nil, fmt.Errorf("unknown analytics operation")
	}
	type usage struct {
		bytes int64
		count int
	}
	groups := map[string]usage{}
	for _, object := range objects {
		key := aws.ToString(object["key"].(*string))
		relative := strings.TrimPrefix(key, a.Prefix)
		prefix, _, found := strings.Cut(relative, "/")
		if found {
			prefix += "/"
		} else {
			prefix = ""
		}
		prefix = a.Prefix + prefix
		group := groups[prefix]
		group.bytes += object["size"].(int64)
		group.count++
		groups[prefix] = group
	}
	names := make([]string, 0, len(groups))
	for prefix := range groups {
		names = append(names, prefix)
	}
	sort.Strings(names)
	rows := []map[string]any{}
	for _, prefix := range names {
		g := groups[prefix]
		rows = append(rows, map[string]any{"prefix": prefix, "bytes": g.bytes, "count": g.count})
	}
	// Prefix aggregation is bounded by max_scan. Return pages of aggregate rows.
	offset := a.Offset
	if offset < 0 || offset > len(rows) {
		return nil, fmt.Errorf("invalid result_offset")
	}
	end := min(offset+1000, len(rows))
	result["prefixes"] = rows[offset:end]
	if end < len(rows) {
		result["next_result_offset"] = end
	}
	return result, nil
}
func unfinished(ctx context.Context, c *s3core.Client, a s3core.Args, max int) (map[string]any, error) {
	hours := a.OlderThanHours
	if hours == 0 {
		hours = 24
	}
	if hours < 0 {
		return nil, fmt.Errorf("older_than_hours must be nonnegative")
	}
	cutoff := time.Now().Add(-time.Duration(hours * float64(time.Hour)))
	token, uploadID := a.Token, a.UploadID
	scanned := 0
	uploads := []map[string]any{}
	for scanned < max && len(uploads) < 1000 {
		args := a
		args.MaxKeys = int32(min(1000, max-scanned, 1000-len(uploads))) //nolint:gosec // Loop guarantees both remaining counts are positive, and min caps the result at 1000.
		args.Token = token
		args.UploadID = uploadID
		raw, e := c.Execute(ctx, "s3_list_multipart_uploads", args, nil)
		if e != nil {
			return nil, e
		}
		page := raw.(map[string]any)
		items := page["uploads"].([]types.MultipartUpload)
		scanned += len(items)
		for _, v := range items {
			if v.Initiated != nil && v.Initiated.Before(cutoff) {
				uploads = append(uploads, map[string]any{"key": v.Key, "upload_id": v.UploadId, "initiated": v.Initiated})
			}
		}
		next := aws.ToString(page["continuation_token"].(*string))
		id := aws.ToString(page["upload_id_marker"].(*string))
		if next == "" {
			token = ""
			uploadID = ""
			break
		}
		if next == token && id == uploadID {
			return nil, fmt.Errorf("S3 repeated upload continuation token")
		}
		token, uploadID = next, id
	}
	return map[string]any{"uploads": uploads, "scanned": scanned, "continuation_token": token, "upload_id_marker": uploadID, "partial": token != ""}, nil
}

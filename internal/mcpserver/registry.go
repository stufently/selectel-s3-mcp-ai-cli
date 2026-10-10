package mcpserver

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/config"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/provider"
	"strings"
)

type ToolSpec struct {
	Name, Levels, Capability, Fields, Required, Description string
	FileRoot                                                bool
}

var registry = []ToolSpec{
	{Name: "s3_list_buckets", Fields: "max_keys continuation_token", Description: "List visible buckets with pagination."},
	{Name: "s3_list_objects", Fields: "bucket prefix max_keys continuation_token", Required: "bucket", Description: "List a scoped prefix, at most 1000 objects per page."},
	{Name: "s3_head_object", Fields: "bucket key version_id", Required: "bucket key", Description: "Read object metadata."},
	{Name: "s3_get_object", Fields: "bucket key version_id max_bytes encoding", Required: "bucket key", Description: "Read at most 262144 bytes inline as text/base64; use presign for larger objects."},
	{Name: "s3_preview_object", Fields: "bucket key version_id max_bytes encoding", Required: "bucket key", Description: "Preview first at most 65536 bytes, detected type and CSV header."},
	{Name: "s3_presign_get", Fields: "bucket key version_id expires_seconds", Required: "bucket key", Description: "Create signed GET URL; default 3600 seconds, provider maximum is unverified."},
	{Name: "s3_list_object_versions", Capability: "versioning", Fields: "bucket prefix max_keys continuation_token version_marker", Required: "bucket", Description: "List versions and delete markers with both pagination markers."},
	{Name: "s3_list_multipart_uploads", Fields: "bucket prefix max_keys continuation_token upload_id", Required: "bucket", Description: "List unfinished uploads; pass both returned markers."},
	{Name: "s3_list_parts", Fields: "bucket key upload_id max_keys continuation_token", Required: "bucket key upload_id", Description: "List multipart parts."},
	{Name: "s3_get_bucket_config", Fields: "bucket", Required: "bucket", Description: "Read location, versioning, CORS, policy and Object Lock; report absent/unsupported configuration."},
	{Name: "s3_largest_objects", Fields: "bucket prefix max_scan limit continuation_token", Required: "bucket", Description: "Bounded largest objects scan, default max_scan 10000; partial results carry continuation token."},
	{Name: "s3_prefix_usage", Fields: "bucket prefix max_scan continuation_token result_offset", Required: "bucket", Description: "Bounded usage by first-level prefixes; paginate aggregate rows with next_result_offset."},
	{Name: "s3_unfinished_uploads", Fields: "bucket prefix max_scan older_than_hours continuation_token upload_id", Required: "bucket", Description: "Find uploads older than N hours (default 24), bounded by max_scan."},
	{Name: "s3_get_object_tagging", Capability: "object_tagging", Fields: "bucket key version_id", Required: "bucket key", Description: "Get object tags."},
	{Name: "s3_download_file", FileRoot: true, Fields: "bucket key version_id path", Required: "bucket key path", Description: "Stream object into a relative file beneath configured file root."},
	{Name: "s3_put_object", Levels: "write", Fields: "bucket key data encoding content_type max_bytes overwrite confirm", Required: "bucket key data", Description: "Put at most 262144 bytes. Default refuses existing targets; overwrite requires destructive approval."},
	{Name: "s3_create_folder", Levels: "write", Fields: "bucket key", Required: "bucket key", Description: "Create a new zero-byte trailing-slash marker; never overwrite."},
	{Name: "s3_copy_object", Levels: "write", Fields: "bucket key source_bucket source_key overwrite confirm", Required: "bucket key source_bucket source_key", Description: "Server-side copy; checks both scopes and prevents unapproved overwrite."},
	{Name: "s3_presign_put", Levels: "write", Fields: "bucket key expires_seconds content_type confirm", Required: "bucket key", Description: "Signed PUT can overwrite data and always requires destructive approval."},
	{Name: "s3_multipart_create", Levels: "write", Capability: "multipart", Fields: "bucket key content_type", Required: "bucket key", Description: "Begin multipart upload."},
	{Name: "s3_multipart_presign_part", Levels: "write", Capability: "multipart", Fields: "bucket key upload_id part_number expires_seconds", Required: "bucket key upload_id part_number", Description: "Sign an upload part; bytes bypass the model."},
	{Name: "s3_multipart_complete", Levels: "write", Capability: "multipart", Fields: "bucket key upload_id parts overwrite confirm", Required: "bucket key upload_id parts", Description: "Complete ordered parts; HEAD target first and use conditional write unless overwrite approved."},
	{Name: "s3_put_object_tagging", Levels: "write", Capability: "object_tagging", Fields: "bucket key version_id tags", Required: "bucket key tags", Description: "Set object tags."},
	{Name: "s3_upload_file", Levels: "write", FileRoot: true, Fields: "bucket key path part_size content_type overwrite confirm", Required: "bucket key path", Description: "Multipart upload from relative file inside file root; overwrite requires destructive approval."},
	{Name: "s3_delete_object", Levels: "delete", Fields: "bucket key version_id confirm", Required: "bucket key", Description: "Delete object or specified version; destructive approval required."},
	{Name: "s3_delete_objects", Levels: "delete", Fields: "bucket objects confirm", Required: "bucket objects", Description: "Delete 1..1000 scoped objects; approval required."},
	{Name: "s3_move_object", Levels: "write delete", Fields: "bucket key source_bucket source_key overwrite confirm", Required: "bucket key source_bucket source_key", Description: "Copy then delete source after successful copy; destructive approval required."},
	{Name: "s3_multipart_abort", Levels: "delete", Capability: "multipart", Fields: "bucket key upload_id confirm", Required: "bucket key upload_id", Description: "Abort incomplete upload; destructive approval required."},
	{Name: "s3_create_bucket", Levels: "admin", Fields: "bucket object_lock", Required: "bucket", Description: "Create bucket. Storage class is chosen in provider panel, not through this tool."},
	{Name: "s3_delete_bucket", Levels: "admin delete", Fields: "bucket confirm", Required: "bucket", Description: "Delete empty bucket only; approval required."},
	{Name: "s3_put_bucket_versioning", Levels: "admin", Capability: "versioning", Fields: "bucket status confirm", Required: "bucket status", Description: "Set Enabled or Suspended; suspension requires destructive approval."},
	{Name: "s3_put_bucket_cors", Levels: "admin", Capability: "cors", Fields: "bucket cors_rules", Required: "bucket cors_rules", Description: "Set bucket CORS rules."},
	{Name: "s3_put_bucket_policy", Levels: "admin", Capability: "bucket_policy", Fields: "bucket policy confirm", Required: "bucket policy", Description: "Set policy JSON; destructive approval required."},
	{Name: "s3_delete_bucket_policy", Levels: "admin", Capability: "bucket_policy", Fields: "bucket confirm", Required: "bucket", Description: "Remove policy; destructive approval required."},
	{Name: "s3_put_object_lock_configuration", Levels: "admin", Capability: "object_lock_config", Fields: "bucket lock_configuration confirm", Required: "bucket lock_configuration", Description: "Update Object Lock configuration; destructive approval required."},
}

func enabled(spec ToolSpec, p provider.Profile, c config.Config) bool {
	for _, level := range strings.Fields(spec.Levels) {
		if !c.Allow[level] {
			return false
		}
	}
	if spec.Capability != "" && !p.Features[spec.Capability] {
		return false
	}
	return !spec.FileRoot || c.FileRoot != ""
}
func Tools(p provider.Profile, c config.Config) []ToolSpec {
	out := []ToolSpec{}
	for _, spec := range registry {
		if enabled(spec, p, c) {
			out = append(out, spec)
		}
	}
	return out
}
func inputSchema(spec ToolSpec) *jsonschema.Schema {
	props := map[string]*jsonschema.Schema{}
	for _, field := range strings.Fields(spec.Fields) {
		s := &jsonschema.Schema{Type: "string"}
		switch field {
		case "overwrite", "confirm", "object_lock":
			s.Type = "boolean"
		case "max_keys", "max_bytes", "expires_seconds", "max_scan", "limit", "part_number", "part_size", "result_offset":
			s.Type = "integer"
			s.Minimum = new(float64)
		case "older_than_hours":
			s.Type = "number"
			s.Minimum = new(float64)
		case "objects", "parts", "cors_rules":
			s.Type = "array"
			s.Items = &jsonschema.Schema{Type: "object"}
		case "tags", "lock_configuration":
			s.Type = "object"
		}
		props[field] = s
	}
	return &jsonschema.Schema{Type: "object", Properties: props, Required: strings.Fields(spec.Required), AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
}

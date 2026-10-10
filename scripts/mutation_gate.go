//go:build ignore

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
)

type mutation struct{ id, file, old, new, pkg, test, assert string }

// Mutation writes and signal restoration share a lock, so restoration cannot
// race with a delayed write of the mutant.
type restoration struct {
	mu       sync.Mutex
	file     string
	data     []byte
	mode     os.FileMode
	hash     [32]byte
	stopping bool
}

func (r *restoration) apply(file string, original, changed []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopping {
		return context.Canceled
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	r.file, r.data, r.mode, r.hash = file, original, info.Mode().Perm(), sha256.Sum256(original)
	return os.WriteFile(file, changed, r.mode)
}

func (r *restoration) restore() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restoreLocked()
}

func (r *restoration) stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopping = true
	return r.restoreLocked()
}

func (r *restoration) restoreLocked() error {
	if r.file == "" {
		return nil
	}
	if err := os.WriteFile(r.file, r.data, r.mode); err != nil {
		return err
	}
	actual, err := os.ReadFile(r.file)
	if err != nil {
		return err
	}
	if sha256.Sum256(actual) != r.hash {
		return fmt.Errorf("restoration hash mismatch: %s", r.file)
	}
	r.file, r.data = "", nil
	return nil
}

func run(ctx context.Context, m mutation, rollback *restoration, selftest string, stopped <-chan struct{}) bool {
	original, e := os.ReadFile(m.file)
	if e != nil {
		panic(e)
	}
	defer func() {
		if e := rollback.restore(); e != nil {
			panic(e)
		}
	}()
	test := func() ([]byte, error) {
		return exec.CommandContext(ctx, "go", "test", "-count=1", m.pkg, "-run", "^"+m.test+"$").CombinedOutput()
	}
	clean, e := test()
	if e != nil {
		fmt.Printf("%s: clean test failed\n%s", m.id, clean)
		return false
	}
	if count := strings.Count(string(original), m.old); count != 1 {
		fmt.Printf("%s: fragment count=%d (must be 1)\n", m.id, count)
		return false
	}
	changed := strings.Replace(string(original), m.old, m.new, 1)
	if selftest == "EARLY" {
		fmt.Printf("%s: selftest sending SIGTERM before apply\n", m.id)
		if e := syscall.Kill(os.Getpid(), syscall.SIGTERM); e != nil {
			panic(e)
		}
		// stop() must finish under the shared mutex before apply() is called.
		<-stopped
	}
	if e = rollback.apply(m.file, original, []byte(changed)); e != nil {
		if selftest == "EARLY" && e == context.Canceled {
			fmt.Println("apply refused: stopping")
			return false
		}
		panic(e)
	}
	if selftest == "EARLY" {
		fmt.Println("early signal failed to prevent mutation")
		return false
	}
	if selftest == "TERM" || selftest == "INT" {
		sig := syscall.SIGTERM
		if selftest == "INT" {
			sig = syscall.SIGINT
		}
		fmt.Printf("%s: applied; selftest sending SIG%s\n", m.id, selftest)
		if e := syscall.Kill(os.Getpid(), sig); e != nil {
			panic(e)
		}
		// The signal handler restores the snapshot and exits nonzero.
		select {}
	}
	result, e := test()
	if e2 := rollback.restore(); e2 != nil {
		panic(e2)
	}
	linePattern := regexp.MustCompile(`(?m)^\s+[a-zA-Z0-9_]+_test\.go:[0-9]+:.*`)
	for _, line := range linePattern.FindAllString(string(result), -1) {
		if e != nil && strings.Contains(line, m.assert) && strings.Contains(string(result), "--- FAIL: "+m.test) {
			fmt.Printf("%s killed: %s\n", m.id, strings.TrimSpace(line))
			return true
		}
	}
	fmt.Printf("%s survived or failed outside its assertion:\n%s", m.id, result)
	return false
}
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rollback := new(restoration)
	selftest := os.Getenv("MUTATION_GATE_SELFTEST_SIGNAL")
	if selftest == "1" {
		selftest = "TERM"
	}
	stopped := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case sig := <-signals:
			if err := rollback.stop(); err != nil {
				fmt.Fprintf(os.Stderr, "%s: restoration failed: %v\n", sig, err)
			} else {
				fmt.Fprintf(os.Stderr, "%s: original restored\n", sig)
			}
			cancel()
			close(stopped)
			if selftest == "EARLY" {
				// Let run() prove that apply() refuses the delayed write.
				return
			}
			os.Exit(1)
		case <-ctx.Done():
		}
	}()
	mutations := []mutation{
		{"M1", "internal/mcpserver/registry.go", "if !c.Allow[level] {", "if false && !c.Allow[level] {", "./internal/mcpserver", "TestRegistration", "REGISTRATION:"},
		{"J05", "internal/mcpserver/server.go", "\t\t\tif e := validateArgumentKeys(req.Params.Arguments); e != nil {\n\t\t\t\treturn failure(fmt.Errorf(\"invalid tool arguments: %w\", e))\n\t\t\t}", "\t\t\tif !strings.Contains(spec.Name, \"copy\") && !strings.Contains(spec.Name, \"move\") {\n\t\t\t\tif e := validateArgumentKeys(req.Params.Arguments); e != nil {\n\t\t\t\t\treturn failure(fmt.Errorf(\"invalid tool arguments: %w\", e))\n\t\t\t\t}\n\t\t\t}", "./internal/mcpserver", "TestEveryToolRejectsCaseCollidingKeys", "ALL_TOOL_KEYS:"},
		{"J12", "internal/cli/cli.go", "o.AllowAdminChanged = cmd.Flags().Changed(\"allow-admin\")", "o.AllowAdminChanged = cmd.Flags().Changed(\"allow-admin\") || cmd.Flags().Changed(\"allow-write\")", "./internal/install", "TestInstallPermissionMatrix", "INSTALL_MATRIX:"},
		{"J16", "internal/s3core/objects.go", "if aws.ToString(head.ETag) == \"\" {", "if strings.Trim(aws.ToString(head.ETag), \"\\\"\") == \"\" {", "./internal/s3core", "TestMoveETagPassthrough", "MOVE_ETAG:"},
		{"J18", "internal/s3core/objects.go", "sourceETag = head.ETag", "sourceETag = aws.String(strings.ToLower(aws.ToString(head.ETag)))", "./internal/s3core", "TestMoveETagPassthrough", "MOVE_ETAG:"},
		{"J19", "internal/s3core/objects.go", "response.HTTPStatusCode() == http.StatusPreconditionFailed", "response.HTTPStatusCode() == http.StatusPreconditionFailed || response.HTTPStatusCode() == http.StatusConflict", "./internal/s3core", "TestMoveDeleteStatusMatrix", "MOVE_DELETE_STATUS:"},
		{"J21", "internal/s3core/client.go", "http.StatusTooManyRequests: {}", "http.StatusTooManyRequests: {}, http.StatusConflict: {}", "./internal/s3core", "TestRetryStatusMatrix", "RETRY_STATUS:"},
		{"J28", "internal/install/install.go", "strings.TrimPrefix(string(encoded), \"[mcp_servers]\\n\")", "strings.TrimPrefix(string(encoded), \"[mcp_servers]\\n\") + \"\\n\"", "./internal/install", "TestCodexManagedBlockExactBytes", "MANAGED_EXACT_BYTES:"},
		{"H04", "internal/mcpserver/arguments.go", "\t\t\tfor _, previous := range keys {\n\t\t\t\tif strings.EqualFold(previous, key) {\n\t\t\t\t\treturn fmt.Errorf(\"case-colliding keys %q and %q\", previous, key)\n\t\t\t\t}\n\t\t\t}", "\t\t\tif len(keys) > 0 && strings.EqualFold(keys[len(keys)-1], key) {\n\t\t\t\treturn fmt.Errorf(\"case-colliding keys %q and %q\", keys[len(keys)-1], key)\n\t\t\t}", "./internal/mcpserver", "TestArgumentsRejectNonAdjacentCollisions", "NONADJACENT_KEYS:"},
		{"H06", "internal/mcpserver/arguments.go", "if _, err := d.Token(); err != io.EOF {", "if _, err := d.Token(); false && err != io.EOF {", "./internal/mcpserver", "TestArgumentsRejectTrailingJSONValue", "ONE_JSON_VALUE:"},
		{"H07", "internal/mcpserver/server.go", "\t\t\tif e := validateArgumentKeys(req.Params.Arguments); e != nil {\n\t\t\t\treturn failure(fmt.Errorf(\"invalid tool arguments: %w\", e))\n\t\t\t}", "\t\t\tif spec.Name == \"s3_delete_object\" || spec.Name == \"s3_delete_objects\" {\n\t\t\t\tif e := validateArgumentKeys(req.Params.Arguments); e != nil {\n\t\t\t\t\treturn failure(fmt.Errorf(\"invalid tool arguments: %w\", e))\n\t\t\t\t}\n\t\t\t}", "./internal/mcpserver", "TestArgumentsValidateOtherTools", "ALL_TOOL_KEYS:"},
		{"H10", "internal/config/config.go", "{\"write\": o.AllowWriteChanged, \"delete\": o.AllowDeleteChanged, \"admin\": o.AllowAdminChanged}", "{\"write\": false, \"delete\": o.AllowDeleteChanged, \"admin\": o.AllowAdminChanged}", "./internal/config", "TestExplicitWriteAndAdminDenials", "ALLOW_DENIAL:"},
		{"H11", "internal/config/config.go", "{\"write\": o.AllowWriteChanged, \"delete\": o.AllowDeleteChanged, \"admin\": o.AllowAdminChanged}", "{\"write\": o.AllowWriteChanged, \"delete\": o.AllowDeleteChanged, \"admin\": false}", "./internal/config", "TestExplicitWriteAndAdminDenials", "ALLOW_DENIAL:"},
		{"H12", "internal/cli/cli.go", "o.AllowWriteChanged = cmd.Flags().Changed(\"allow-write\")", "o.AllowWriteChanged = false", "./internal/install", "TestInstallPersistsWriteAndAdminPermissions", "INSTALL_PERMISSIONS:"},
		{"H13", "internal/cli/cli.go", "o.AllowAdminChanged = cmd.Flags().Changed(\"allow-admin\")", "o.AllowAdminChanged = false", "./internal/install", "TestInstallPersistsWriteAndAdminPermissions", "INSTALL_PERMISSIONS:"},
		{"H14", "internal/cli/cli.go", "fmt.Sprintf(\"--allow-%s=%t\", level, c.Allow[level])", "fmt.Sprintf(\"--allow-%s=%t\", level, c.Allow[level] && level != \"admin\")", "./internal/install", "TestInstallPersistsWriteAndAdminPermissions", "INSTALL_PERMISSIONS:"},
		{"H18", "internal/s3core/objects.go", "if aws.ToString(head.ETag) == \"\" {", "if false && aws.ToString(head.ETag) == \"\" {", "./internal/s3core", "TestMoveRequiresSourceETag", "MOVE_MISSING_ETAG:"},
		{"H19", "internal/s3core/objects.go", "sourceETag = head.ETag", "sourceETag = aws.String(\"\\\"initial\\\"\")", "./internal/s3core", "TestMoveUsesObservedETag", "MOVE_OBSERVED_ETAG:"},
		{"H20", "internal/s3core/objects.go", "response.HTTPStatusCode() == http.StatusPreconditionFailed", "response.HTTPStatusCode() >= 400", "./internal/s3core", "TestMoveReportsDeletionFailure", "MOVE_DELETE_FAILURE:"},
		{"H22", "internal/s3core/client.go", "http.StatusTooManyRequests: {}", "http.StatusTooManyRequests: {}, http.StatusRequestTimeout: {}", "./internal/s3core", "TestDoesNotRetryHTTP408", "NO_RETRY_408:"},
		{"H28", "internal/cli/transfers.go", "\tif args.Key != \"\" {\n\t\targs.Key = strings.TrimSuffix(args.Key, \"/\") + \"/\"\n\t}\n", "", "./internal/cli", "TestUploadTreeAddsPrefixSeparator", "UPLOAD_SEPARATOR:"},
		{"H29", "internal/cli/transfers.go", "os.OpenRoot(source)", "os.OpenRoot(\".\")", "./internal/cli", "TestRecursiveCopyReadsSelectedDirectory", "RECURSIVE_SELECTED:"},
		{"H32", "internal/install/install.go", "strings.TrimPrefix(string(encoded), \"[mcp_servers]\\n\")", "strings.TrimPrefix(string(encoded), \"[mcp_servers]\")", "./internal/install", "TestCodexManagedBlockStartsWithChildTable", "MANAGED_BLOCK_BODY:"},
		{"M2", "internal/safety/gate.go", "if confirm {", "if true {", "./internal/safety", "TestGateMatrix", "GATE: policy=confirm"},
		{"M3", "internal/safety/gate.go", "case \"block\":\n\t\treturn fmt.Errorf(\"blocked: %s\", effect)", "case \"block\":\n\t\treturn nil", "./internal/safety", "TestGateMatrix", "GATE: policy=block"},
		{"M4", "internal/safety/gate.go", "if g.Policy == \"elicit\" {", "if false && g.Policy == \"elicit\" {", "./internal/safety", "TestGateMatrix", "GATE: policy=elicit"},
		{"M5", "internal/safety/scope.go", "strings.HasPrefix(k, p+\"/\")", "strings.HasPrefix(k, p)", "./internal/safety", "TestScope", "SCOPE:"},
		{"M6", "internal/safety/files.go", "return f.root.Open(path)", "return os.Open(filepath.Join(f.root.Name(), path))", "./internal/safety", "TestFileRoot", "FS_READ:"},
		{"M7", "internal/safety/redact.go", "Signature|Credential|Security-Token", "Ignored|Credential|Security-Token", "./internal/safety", "TestRedaction", "REDACT:"},
		{"M8", "internal/mcpserver/registry.go", "spec.Capability != \"\" && !p.Features[spec.Capability]", "false && spec.Capability != \"\" && !p.Features[spec.Capability]", "./internal/mcpserver", "TestCapabilities", "CAPABILITY:"},
		{"M9", "internal/s3core/objects.go", "Body: bytes.NewReader(data), ContentType: optional(a.ContentType), IfNoneMatch: c.condition(a)", "Body: bytes.NewReader(data), ContentType: optional(a.ContentType), IfNoneMatch: nil", "./internal/s3core", "TestConditionalPut", "CONDITIONAL:"},
		{"M10", "internal/config/config.go", "return mode.Perm() & ^os.FileMode(0600) == 0", "return true", "./internal/config", "TestCredentialPermissions", "PERMISSIONS:"},
		{"M11", "internal/s3core/multipart.go", "if e := c.ensureAbsent(ctx, a); e != nil {\n\t\t\treturn nil, e\n\t\t}", "// mutation: target check removed", "./internal/s3core", "TestMultipartExistingTarget", "MULTIPART_EXISTS:"},
		{"M12", "internal/s3core/client.go", "return a.Overwrite", "return false", "./internal/s3core", "TestOverwriteBlocked", "OVERWRITE_GATE:"},
		{"M13", "internal/safety/confirmation.go", "if !hmac.Equal(signature, mac.Sum(nil)) {", "if false && !hmac.Equal(signature, mac.Sum(nil)) {", "./internal/mcpserver", "TestConfirmationSignature", "STATE_SIGNATURE:"},
		{"M14", "internal/safety/confirmation.go", "if state.Digest != digest {", "if false && state.Digest != digest {", "./internal/mcpserver", "TestConfirmationArguments", "STATE_ARGUMENTS:"},
		{"M15", "internal/safety/confirmation.go", "delete(c.pending, state.Nonce)", "// mutation: nonce is not consumed", "./internal/mcpserver", "TestConfirmationNonce", "STATE_NONCE:"},
		{"M16", "internal/safety/confirmation.go", "if c.now().Unix() >= state.ExpiresAt {", "if false && c.now().Unix() >= state.ExpiresAt {", "./internal/mcpserver", "TestConfirmationExpired", "STATE_EXPIRED:"},
		{"M17", "internal/safety/confirmation.go", "state.Tool != tool", "false && state.Tool != tool", "./internal/mcpserver", "TestConfirmationToolChanged", "STATE_TOOL:"},
		{"M18", "internal/mcpserver/server.go", "response.Action != \"accept\"", "false && response.Action != \"accept\"", "./internal/mcpserver", "TestConfirmationDeclinedCannotBeReused", "STATE_DECLINED:"},
		{"G01", "internal/safety/gate.go", "if e != nil {\n\t\t\t\treturn fmt.Errorf(\"confirmation failed: %w\", e)\n\t\t\t}", "if e != nil {\n\t\t\t\treturn nil\n\t\t\t}", "./internal/safety", "TestGateElicitationError", "GATE_ERROR:"},
		{"G02", "internal/safety/gate.go", "default:\n\t\treturn fmt.Errorf(\"invalid gate policy\")", "default:\n\t\treturn nil", "./internal/safety", "TestGateInvalidPolicy", "GATE_POLICY:"},
		{"C01", "internal/safety/confirmation.go", "if len(token) > 4096 {", "if len(token) > 1048576 {", "./internal/safety", "TestConfirmationTokenLimit", "TOKEN_LIMIT:"},
		{"C02", "internal/safety/confirmation.go", "ExpiresAt: now.Add(5 * time.Minute).Unix()", "ExpiresAt: now.Add(4 * time.Minute).Unix()", "./internal/safety", "TestConfirmationFiveMinuteWindow", "FIVE_MINUTES:"},
		{"C03", "internal/safety/confirmation.go", "decoder.UseNumber()", "if false { decoder.UseNumber() }", "./internal/safety", "TestConfirmationIntegerPrecision", "INTEGER_DIGEST:"},
		{"C04", "internal/safety/confirmation.go", "if !hmac.Equal(signature, mac.Sum(nil)) {", "if len(signature) == 0 || signature[0] != mac.Sum(nil)[0] {", "./internal/mcpserver", "TestConfirmationSignature", "STATE_SIGNATURE:"},
		{"SCP02", "internal/safety/scope.go", "if b == \"\" {\n\t\treturn fmt.Errorf(\"bucket is required\")\n\t}", "if false && b == \"\" {\n\t\treturn fmt.Errorf(\"bucket is required\")\n\t}", "./internal/safety", "TestScopeRequiresBucket", "EMPTY_BUCKET:"},
		{"F01", "internal/safety/files.go", "os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600", "os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644", "./internal/safety", "TestDownloadFilePrivatePermissions", "DOWNLOAD_MODE:"},
		{"F02", "internal/safety/files.go", "if !filepath.IsLocal(path) || strings.Contains(path, \"\\\\\") {", "if !filepath.IsLocal(path) {", "./internal/safety", "TestFilePathValidationReasons", "FILE_PATH:"},
		{"F03", "internal/safety/files.go", "if !filepath.IsLocal(path) || strings.Contains(path, \"\\\\\") {", "if strings.Contains(path, \"\\\\\") {", "./internal/safety", "TestFilePathValidationReasons", "FILE_PATH:"},
		{"RD01", "internal/safety/redact.go", "(?i)([?&]X-Amz-(?:Signature|Credential|Security-Token)=)", "([?&]X-Amz-(?:Signature|Credential|Security-Token)=)", "./internal/safety", "TestRedactionLowercaseAndEmptySecret", "REDACT_LOWER:"},
		{"RD02", "internal/safety/redact.go", "if secret != \"\" {", "if true {", "./internal/safety", "TestRedactionLowercaseAndEmptySecret", "REDACT_EMPTY:"},
		{"CFG02", "internal/config/config.go", "if o.Policy != \"\" {\n\t\tc.Policy = o.Policy\n\t}", "if false && o.Policy != \"\" {\n\t\tc.Policy = o.Policy\n\t}", "./internal/config", "TestOverridePolicyAndFileRoot", "POLICY_OVERRIDE:"},
		{"CFG03", "internal/config/config.go", "if !info.Mode().IsRegular() || !privatePermissions(info.Mode()) {", "if !info.Mode().IsRegular() {", "./internal/config", "TestOpenedCredentialPermissions", "OPENED_PERMISSIONS:"},
		{"CFG05", "internal/config/config.go", "if o.FileRoot != \"\" {\n\t\tc.FileRoot = o.FileRoot\n\t}", "if false && o.FileRoot != \"\" {\n\t\tc.FileRoot = o.FileRoot\n\t}", "./internal/config", "TestOverridePolicyAndFileRoot", "ROOT_OVERRIDE:"},
		{"CFG06", "internal/config/config.go", "if enabled || changed[level] {", "if false && (enabled || changed[level]) {", "./internal/config", "TestAllowFlagsGrantLevels", "ALLOW_GRANT:"},
		{"SRV02", "internal/mcpserver/server.go", "ip.Capabilities.Elicitation.Form != nil || ip.Capabilities.Elicitation.URL == nil", "ip.Capabilities.Elicitation.Form != nil && ip.Capabilities.Elicitation.URL == nil", "./internal/mcpserver", "TestLegacyFormElicitation", "LEGACY_FORM:"},
		{"SRV03", "internal/mcpserver/server.go", "response.Content[\"approved\"] != true", "false && response.Content[\"approved\"] != true", "./internal/mcpserver", "TestConfirmFormRequiresApprovedTrue", "APPROVED_TRUE:"},
		{"SRV04", "internal/mcpserver/server.go", "if len(data) > int(s3core.InlineLimit) {", "if len(data) > int(s3core.InlineLimit)*8 {", "./internal/mcpserver", "TestLargeResultTextSummary", "RESULT_SUMMARY:"},
		{"REG01", "internal/mcpserver/registry.go", "return !spec.FileRoot || c.FileRoot != \"\"", "return !spec.FileRoot", "./internal/mcpserver", "TestFileToolsRegistration", "FILE_TOOLS:"},
		{"REG03", "internal/mcpserver/registry.go", "AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}", "AdditionalProperties: &jsonschema.Schema{}", "./internal/mcpserver", "TestToolRejectsAdditionalProperties", "EXTRA_PROPERTY:"},
		{"CL01", "internal/s3core/client.go", "\"s3_multipart_abort\", \"s3_delete_bucket\", \"s3_put_bucket_policy\"", "\"s3_multipart_abort\", \"s3_put_bucket_policy\"", "./internal/s3core", "TestAdditionalDestructiveGates", "ADDITIONAL_GATE:"},
		{"CL02", "internal/s3core/client.go", "return a.Status == \"Suspended\"", "return false", "./internal/s3core", "TestAdditionalDestructiveGates", "ADDITIONAL_GATE:"},
		{"CL03", "internal/s3core/client.go", "if a.Overwrite {\n\t\treturn nil\n\t}", "if false && a.Overwrite {\n\t\treturn nil\n\t}", "./internal/s3core", "TestApprovedOverwriteSkipsAbsenceCheck", "OVERWRITE_ALLOWED:"},
		{"CL05", "internal/s3core/client.go", "if n == 0 {\n\t\treturn 1000, nil\n\t}", "if n == 0 {\n\t\treturn 100, nil\n\t}", "./internal/s3core", "TestListPageBounds", "PAGE_SIZE:"},
		{"CL06", "internal/s3core/client.go", "if n < 1 || n > 1000 {", "if n < 1 || n > 10000 {", "./internal/s3core", "TestListPageBounds", "PAGE_BOUND:"},
		{"CL07", "internal/s3core/client.go", "if n == 0 {\n\t\tn = 3600\n\t}", "if n == 0 {\n\t\tn = 60\n\t}", "./internal/s3core", "TestPresignExpiryBounds", "PRESIGN_EXPIRY:"},
		{"CL08", "internal/s3core/client.go", "const InlineLimit int64 = 262144", "const InlineLimit int64 = 131072", "./internal/s3core", "TestInlinePutLiteralBoundary", "INLINE_BOUNDARY:"},
		{"CL09", "internal/s3core/client.go", "if key == p {\n\t\t\t\t\treturn fmt.Errorf(\"scope_denied: listing requires prefix %s/\", p)\n\t\t\t\t}", "if false && key == p {\n\t\t\t\t\treturn fmt.Errorf(\"scope_denied: listing requires prefix %s/\", p)\n\t\t\t\t}", "./internal/s3core", "TestScopeAndSourceGuardReasons", "GUARD_REASON:"},
		{"CL10", "internal/s3core/client.go", "func wholeBucket(name string) bool {\n\treturn name == \"s3_get_bucket_config\" || name == \"s3_create_bucket\" || name == \"s3_delete_bucket\" || strings.HasPrefix(name, \"s3_put_bucket_\") || name == \"s3_delete_bucket_policy\" || name == \"s3_put_object_lock_configuration\"\n}", "func wholeBucket(name string) bool {\n\treturn strings.HasPrefix(name, \"not-a-bucket-tool\")\n}", "./internal/s3core", "TestScopeAndSourceGuardReasons", "GUARD_REASON:"},
		{"CL11", "internal/s3core/client.go", "if a.Bucket == a.SourceBucket && a.Key == a.SourceKey {\n\t\t\treturn fmt.Errorf(\"source and destination must differ\")\n\t\t}", "if false && a.Bucket == a.SourceBucket && a.Key == a.SourceKey {\n\t\t\treturn fmt.Errorf(\"source and destination must differ\")\n\t\t}", "./internal/s3core", "TestScopeAndSourceGuardReasons", "GUARD_REASON:"},
		{"CL12", "internal/s3core/client.go", "if len(a.Objects) == 0 || len(a.Objects) > 1000 {", "if len(a.Objects) == 0 || len(a.Objects) > 100000 {", "./internal/s3core", "TestDeleteObjectsBoundary", "DELETE_BOUNDARY:"},
		{"CL13", "internal/s3core/client.go", "if !a.Overwrite && c.Profile.Features[\"conditional_writes\"] {", "if !a.Overwrite {", "./internal/s3core", "TestConditionalWritesDisabled", "CONDITIONAL_DISABLED:"},
		{"INS01", "internal/install/install.go", "quote := func(v string) string { return \"'\" + strings.ReplaceAll(v, \"'\", \"'\\\\''\") + \"'\" }", "quote := func(v string) string { return v }", "./internal/install", "TestUserCommandQuotesArguments", "INSTALL_QUOTE:"},
		{"INS02", "internal/install/install.go", "strings.Count(text, begin) != 1 || strings.Count(text, end) != 1", "false", "./internal/install", "TestCodexDuplicateManagedMarkers", "MANAGED_MARKERS:"},
		{"CLI01", "internal/cli/cli.go", "\t\targs.Confirm = true\n\t}\n\tvar out any\n", "\t}\n\tvar out any\n", "./internal/cli", "TestYesApprovesNonTTYDestructiveOperations", "CLI_YES:"},
		{"CLI02", "internal/cli/cli.go", "if a.yes {\n\t\treturn nil\n\t}", "if a.yes && !strings.Contains(effect, \"s3_delete\") && !strings.Contains(effect, \"s3_move\") {\n\t\treturn nil\n\t}", "./internal/cli", "TestYesApprovesNonTTYDestructiveOperations", "CLI_YES:"},
	}
	ok := true
	for i, m := range mutations {
		mode := ""
		if i == 0 {
			mode = selftest
		}
		if !run(ctx, m, rollback, mode, stopped) {
			ok = false
		}
		if ctx.Err() != nil {
			break
		}
	}
	if !ok {
		os.Exit(1)
	}
	fmt.Printf("%d/%d killed; original hashes restored\n", len(mutations), len(mutations))
}

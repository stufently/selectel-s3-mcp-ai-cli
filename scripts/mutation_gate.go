//go:build ignore

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type mutation struct{ id, file, old, new, pkg, test, assert string }

func run(m mutation) bool {
	original, e := os.ReadFile(m.file)
	if e != nil {
		panic(e)
	}
	before := sha256.Sum256(original)
	restored := false
	defer func() {
		if !restored {
			if e := os.WriteFile(m.file, original, 0600); e != nil {
				panic(e)
			}
		}
		actual, e := os.ReadFile(m.file)
		if e != nil || sha256.Sum256(actual) != before {
			panic("restoration hash mismatch: " + m.id)
		}
	}()
	test := func() ([]byte, error) {
		return exec.Command("go", "test", "-count=1", m.pkg, "-run", "^"+m.test+"$").CombinedOutput()
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
	if e = os.WriteFile(m.file, []byte(changed), 0600); e != nil {
		panic(e)
	}
	result, e := test()
	if e2 := os.WriteFile(m.file, original, 0600); e2 != nil {
		panic(e2)
	}
	restored = true
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
	mutations := []mutation{
		{"M1", "internal/mcpserver/registry.go", "if !c.Allow[level] {", "if false && !c.Allow[level] {", "./internal/mcpserver", "TestRegistration", "REGISTRATION:"},
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
	}
	ok := true
	for _, m := range mutations {
		if !run(m) {
			ok = false
		}
	}
	if !ok {
		os.Exit(1)
	}
	fmt.Printf("%d/%d killed; original hashes restored\n", len(mutations), len(mutations))
}

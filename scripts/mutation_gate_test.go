//go:build ignore

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGateSelftestProcess(t *testing.T) {
	if os.Getenv("MUTATION_GATE_TEST_PROCESS") == "1" {
		main()
	}
}

func TestGateSignalSelftests(t *testing.T) {
	for _, mode := range []string{"TERM", "INT", "EARLY", "1"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			pkg := filepath.Join(dir, "internal", "mcpserver")
			if err := os.MkdirAll(pkg, 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("package mcpserver\ntype config struct { Allow map[string]bool }\nfunc allowed(c config, level string) bool {\nif !c.Allow[level] { return false }; return true\n}\n")
			for path, data := range map[string][]byte{
				filepath.Join(dir, "go.mod"):           []byte("module gate-selftest\ngo 1.27\n"),
				filepath.Join(pkg, "registry.go"):      original,
				filepath.Join(pkg, "registry_test.go"): []byte("package mcpserver\nimport \"testing\"\nfunc TestRegistration(t *testing.T) { if allowed(config{Allow: map[string]bool{}}, \"write\") { t.Fatal(\"REGISTRATION: denied level exposed\") } }\n"),
			} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGateSelftestProcess$")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "MUTATION_GATE_TEST_PROCESS=1", "MUTATION_GATE_SELFTEST_SIGNAL="+mode)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil || err == nil || !strings.Contains(string(out), "original restored") || strings.Contains(string(out), "restoration failed") {
				t.Fatalf("SIGNAL_SELFTEST: mode=%s err=%v timeout=%v output=%s", mode, err, ctx.Err(), out)
			}
			if mode == "EARLY" && !strings.Contains(string(out), "apply refused: stopping") {
				t.Fatalf("EARLY_SELFTEST: apply was not refused: %s", out)
			}
			actual, err := os.ReadFile(filepath.Join(pkg, "registry.go"))
			if err != nil || !bytes.Equal(actual, original) || sha256.Sum256(actual) != sha256.Sum256(original) {
				t.Fatalf("SIGNAL_SELFTEST: snapshot changed: err=%v data=%s", err, actual)
			}
		})
	}
}

func TestRestorationRejectsWrongSnapshotHash(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.go")
	original, changed := []byte("original\n"), []byte("mutated\n")
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	r := new(restoration)
	if err := r.apply(file, original, changed); err != nil {
		t.Fatal(err)
	}
	r.hash = sha256.Sum256(changed)
	if err := r.stop(); err == nil || !strings.Contains(err.Error(), "restoration hash mismatch") {
		t.Fatalf("RESTORATION_HASH: wrong snapshot hash was accepted: %v", err)
	}
	actual, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("RESTORATION_HASH: original bytes not written: %s err=%v", actual, err)
	}
}

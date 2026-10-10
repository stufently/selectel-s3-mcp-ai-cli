package safety

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateMatrix(t *testing.T) {
	for _, policy := range []string{"confirm", "elicit", "block", "allow"} {
		for _, client := range []bool{false, true} {
			for _, accept := range []bool{false, true} {
				for _, confirm := range []bool{false, true} {
					var ask Elicitor
					if client {
						ask = func(context.Context, string) (bool, error) { return accept, nil }
					}
					want := policy == "allow" || (policy == "confirm" && ((client && accept) || (!client && confirm))) || (policy == "elicit" && client && accept)
					e := (Gate{Policy: policy}).Check(context.Background(), confirm, ask, "Delete the object permanently")
					if (e == nil) != want {
						t.Fatalf("GATE: policy=%s client=%v accept=%v confirm=%v: %v", policy, client, accept, confirm, e)
					}
				}
			}
		}
	}
}
func TestScope(t *testing.T) {
	s, e := NewScope([]string{"whole"}, []string{"partial/backups/"})
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		b, k        string
		whole, want bool
	}{
		{"partial", "backups/a", false, true}, {"partial", "backups-evil/a", false, false}, {"partial", "backups/../evil", false, false},
		{"partial", "", true, false}, {"partial", "", false, false}, {"partial", "backups", false, true}, {"partial", "back", false, false},
		{"whole", "", true, true}, {"other", "anything", false, false},
	}
	for _, c := range cases {
		if e := s.Check(c.b, c.k, c.whole); (e == nil) != c.want {
			t.Fatalf("SCOPE: %s/%s whole=%v got=%v", c.b, c.k, c.whole, e)
		}
	}
}
func TestFileRoot(t *testing.T) {
	outside := t.TempDir()
	rootdir := t.TempDir()
	if e := os.WriteFile(filepath.Join(outside, "private"), []byte("outside"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(rootdir, "escape")); e != nil {
		t.Fatal(e)
	}
	f, e := OpenFiles(rootdir)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = f.Read("escape/private"); e == nil {
		t.Fatal("FS_READ: escaped root via symlink")
	}
	if e = f.Write("escape/new", strings.NewReader("x")); e == nil {
		t.Fatal("FS_WRITE: escaped root via symlink")
	}
	for _, path := range []string{"../private", "/tmp/private", "a/../b"} {
		if _, e = f.Read(path); e == nil {
			t.Fatal("traversal accepted")
		}
	}
	if e = f.Write("ok", strings.NewReader("data")); e != nil {
		t.Fatal(e)
	}
	in, e := f.Read("ok")
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
}
func TestRedaction(t *testing.T) {
	input := "secret-text https://example.test/a?X-Amz-Signature=abc&X-Amz-Credential=key%2Fscope&X-Amz-Security-Token=token&ok=1"
	got := Redact(input, "secret-text")
	for _, s := range []string{"secret-text", "abc", "key%2Fscope", "token"} {
		if strings.Contains(got, s) {
			t.Fatalf("REDACT: sensitive value survived: %s", s)
		}
	}
	if !strings.Contains(got, "ok=1") {
		t.Fatal("unrelated query removed")
	}
}

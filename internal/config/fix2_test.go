package config

import "testing"

func TestExplicitWriteAndAdminDenials(t *testing.T) {
	p := testProfile(t)
	for _, tc := range []struct {
		name                 string
		env                  string
		o                    Overrides
		write, delete, admin bool
	}{
		{"write", "write,delete", Overrides{AllowWriteChanged: true}, false, true, false},
		{"admin", "write,admin", Overrides{AllowAdminChanged: true}, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(p.EnvPrefix+"ALLOW", tc.env)
			c, err := Load(p, tc.o)
			if err != nil {
				t.Fatal(err)
			}
			if c.Allow["write"] != tc.write || c.Allow["delete"] != tc.delete || c.Allow["admin"] != tc.admin {
				t.Fatalf("ALLOW_DENIAL: %s denial changed wrong levels: %v", tc.name, c.Allow)
			}
		})
	}
}

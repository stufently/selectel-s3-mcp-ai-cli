package safety

import (
	"fmt"
	"strings"
)

type Scope struct {
	Buckets    map[string]bool
	Prefixes   map[string][]string
	Restricted bool
}

func NewScope(buckets, prefixes []string) (Scope, error) {
	s := Scope{Buckets: map[string]bool{}, Prefixes: map[string][]string{}, Restricted: len(buckets) > 0 || len(prefixes) > 0}
	for _, b := range buckets {
		if b == "" || strings.Contains(b, "/") {
			return s, fmt.Errorf("invalid allowed bucket")
		}
		s.Buckets[b] = true
	}
	for _, v := range prefixes {
		b, p, ok := strings.Cut(v, "/")
		if !ok || b == "" || strings.Trim(p, "/") == "" || traversal(p) {
			return s, fmt.Errorf("invalid allowed prefix %q", v)
		}
		s.Prefixes[b] = append(s.Prefixes[b], strings.TrimSuffix(p, "/"))
	}
	return s, nil
}
func traversal(k string) bool {
	for _, v := range strings.Split(k, "/") {
		if v == ".." {
			return true
		}
	}
	return false
}
func (s Scope) BucketVisible(b string) bool {
	return !s.Restricted || s.Buckets[b] || len(s.Prefixes[b]) > 0
}
func (s Scope) Check(b, k string, whole bool) error {
	if b == "" {
		return fmt.Errorf("bucket is required")
	}
	if traversal(k) {
		return fmt.Errorf("scope_denied: '..' key segment")
	}
	if !s.Restricted {
		return nil
	}
	if prefixes := s.Prefixes[b]; len(prefixes) > 0 {
		if !whole {
			for _, p := range prefixes {
				if k == p || strings.HasPrefix(k, p+"/") {
					return nil
				}
			}
		}
		return fmt.Errorf("scope_denied: bucket %s is partially allowed; allowed prefixes: %s", b, strings.Join(prefixes, ", "))
	}
	if s.Buckets[b] {
		return nil
	}
	return fmt.Errorf("scope_denied: bucket %s", b)
}

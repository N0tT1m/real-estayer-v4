package service

import (
	"testing"
)

func TestRandomSlugShape(t *testing.T) {
	for _, n := range []int{8, 16, 24} {
		s, err := randomSlug(n)
		if err != nil {
			t.Fatalf("randomSlug(%d) error: %v", n, err)
		}
		if len(s) != n {
			t.Errorf("randomSlug(%d) returned length %d, want %d", n, len(s), n)
		}
		for _, r := range s {
			// base32 encoding, lowercased: a-z, 2-7
			ok := (r >= 'a' && r <= 'z') || (r >= '2' && r <= '7')
			if !ok {
				t.Errorf("randomSlug returned unexpected char %q in %q", r, s)
			}
		}
	}
}

func TestRandomSlugIsNotDeterministic(t *testing.T) {
	a, _ := randomSlug(16)
	b, _ := randomSlug(16)
	if a == b {
		t.Fatalf("two consecutive slugs collided: %q", a)
	}
}

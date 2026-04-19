package service

import (
	"errors"
	"testing"
)

func TestValidatePasswordStrength(t *testing.T) {
	cases := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"too short", "a1b2c", true},
		{"letters only", "abcdefghij", true},
		{"digits only", "1234567890", true},
		{"no upper", "abc1234567", true},
		{"no lower", "ABC1234567", true},
		{"under length", "Abc12345", true},
		{"too few distinct", "AaAaAa111!", true},
		{"common substring", "Password1234", true},
		{"long mix", "CorrectHorse9Battery", false},
		{"policy met", "Tr0ubad0urs!ng", false},
		{"over max", string(make([]byte, 257)), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidatePasswordStrength(c.pw)
			if (err != nil) != c.wantErr {
				t.Fatalf("ValidatePasswordStrength(%q) error=%v wantErr=%v", c.pw, err, c.wantErr)
			}
			if c.wantErr && err != nil && !errors.Is(err, ErrWeakPassword) {
				t.Errorf("expected ErrWeakPassword, got %v", err)
			}
		})
	}
}

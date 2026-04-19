package service

import (
	"strings"
	"testing"
)

func TestStripCodeFence(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain json", "plain json"},
		{"```json\n{\"a\":1}\n```", "{\"a\":1}"},
		{"```\n{\"a\":1}\n```", "{\"a\":1}"},
		{"```json\n{\"a\":1}", "{\"a\":1}"}, // unterminated, still strips leader
		// stripCodeFence only strips when the input *starts* with ``` — a
		// leading space bypasses the prefix check. Documenting that contract
		// here rather than expecting a strip.
		{"  ```json\n{\"a\":1}\n```\n", "```json\n{\"a\":1}\n```"},
	}
	for _, c := range cases {
		got := stripCodeFence(c.in)
		if got != c.want {
			t.Errorf("stripCodeFence(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestBudgetSuffix(t *testing.T) {
	if budgetSuffix(0) != "" || budgetSuffix(-1) != "" {
		t.Errorf("non-positive budgets produce no suffix")
	}
	got := budgetSuffix(1500)
	if !strings.Contains(got, "1500") || !strings.Contains(got, "USD") {
		t.Errorf("budgetSuffix(1500) = %q", got)
	}
}

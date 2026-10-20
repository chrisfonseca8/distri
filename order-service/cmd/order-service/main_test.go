package main

import "testing"

func TestParseRole(t *testing.T) {
	for _, value := range []string{"api", "worker", "all"} {
		role, err := parseRole(value)
		if err != nil {
			t.Errorf("parseRole(%q): %v", value, err)
		}
		if string(role) != value {
			t.Errorf("parseRole(%q) = %q", value, role)
		}
	}
	if _, err := parseRole("invalid"); err == nil {
		t.Fatal("parseRole accepted an invalid role")
	}
}

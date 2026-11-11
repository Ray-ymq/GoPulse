package config

import "testing"

func TestBusinessIsTheOnlyServiceRole(t *testing.T) {
	for _, raw := range []string{"", "business", " BUSINESS "} {
		role, err := ParseServiceRole(raw)
		if err != nil || role != ServiceRoleBusiness || !role.RunsBusiness() || !role.IsValid() {
			t.Fatalf("role %q = %q, %v", raw, role, err)
		}
	}
	for _, raw := range []string{"combined", "platform", "unknown"} {
		if _, err := ParseServiceRole(raw); err == nil {
			t.Fatalf("removed role %q accepted", raw)
		}
	}
}

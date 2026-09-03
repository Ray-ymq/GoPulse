package user

import "testing"

func TestParseRoleAcceptsOnlyPersistedRoles(t *testing.T) {
	for _, value := range []string{"user", "admin"} {
		role, err := ParseRole(value)
		if err != nil || string(role) != value {
			t.Fatalf("ParseRole(%q) = %q, %v", value, role, err)
		}
	}
	for _, value := range []string{"", "owner", "ADMIN"} {
		if _, err := ParseRole(value); err == nil {
			t.Fatalf("ParseRole(%q) error = nil", value)
		}
	}
}

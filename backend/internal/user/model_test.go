package user

import "testing"

func TestParseRoleAcceptsOnlyPersistedRoles(t *testing.T) {
	for _, value := range []string{"user", "super_admin"} {
		role, err := ParseRole(value)
		if err != nil || string(role) != value {
			t.Fatalf("ParseRole(%q) = %q, %v", value, role, err)
		}
	}
	for _, value := range []string{"", "owner", "ADMIN", "admin"} {
		if _, err := ParseRole(value); err == nil {
			t.Fatalf("ParseRole(%q) error = nil", value)
		}
	}
}

func TestAuditBuildersRejectUnboundedDetails(t *testing.T) {
	if _, err := RoleAuditDetails("user.role.change", RoleUser, RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := RoleAuditDetails("plugin.install", RoleUser, RoleSuperAdmin); err == nil {
		t.Fatal("wrong action accepted")
	}
	if _, err := PluginAuditDetails("plugin.install", "redis-exporter", "1.12.1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := PluginAuditDetails("plugin.install", "redis-exporter", "1.12.1", "password=secret"); err == nil {
		t.Fatal("unbounded detail accepted")
	}
	if _, err := RuleAuditDetails("rule.create", 1, 1, "warning", "metrics"); err != nil {
		t.Fatal(err)
	}
}

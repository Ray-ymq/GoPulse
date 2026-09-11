package http

import "testing"

func TestAuditCursorIsBoundToActorAndSecret(t *testing.T) {
	h := NewManagementHandler(nil, "management-cursor-secret")
	other := NewManagementHandler(nil, "another-secret")
	if h.sign("payload", 1) == h.sign("payload", 2) {
		t.Fatal("cursor is not actor-bound")
	}
	if h.sign("payload", 1) == other.sign("payload", 1) {
		t.Fatal("cursor is not secret-bound")
	}
}

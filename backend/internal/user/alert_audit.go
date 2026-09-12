package user

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// AppendAlertAudit participates in the caller's data transaction.
func AppendAlertAudit(ctx context.Context, tx *sql.Tx, actor uint64, action string, resource uint64, requestID string, details AuditDetails) error {
	if !ValidAuditAction(action) || len(details.encoded) == 0 {
		return errors.New("invalid audit")
	}
	var op [16]byte
	if _, e := rand.Read(op[:]); e != nil {
		return e
	}
	kind := "user"
	var uid any = actor
	if actor == 0 {
		kind = "system"
		uid = nil
	}
	rt := "rule"
	if len(action) > 6 && action[:6] == "alert." {
		rt = "alert"
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO management_audit_events(operation_id,actor_kind,actor_user_id,action,resource_type,resource_id,phase,outcome,request_id,details_json) VALUES(?,?,?,?,?,?,'completed','succeeded',?,?)`, hex.EncodeToString(op[:]), kind, uid, action, rt, resource, requestID, []byte(details.encoded))
	return e
}

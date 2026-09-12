package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

func (r *MySQLRepository) RequestPluginAudit(ctx context.Context, actor uint64, action, plugin, requestID string) (string, error) {
	details, err := PluginAuditDetails(action, plugin, "", "")
	if err != nil {
		return "", err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return "", err
	}
	operation := hex.EncodeToString(token[:])
	_, err = r.database.ExecContext(ctx, `INSERT INTO management_audit_events(operation_id,actor_kind,actor_user_id,action,resource_type,resource_id,phase,outcome,request_id,details_json) VALUES(?,'user',?,?,'plugin',?,'requested','unknown',?,?)`, operation, actor, action, plugin, requestID, []byte(details.encoded))
	return operation, err
}
func (r *MySQLRepository) CompletePluginAudit(ctx context.Context, operation, action, plugin, outcome string) error {
	reason := ""
	if outcome == "failed" {
		reason = "operation_failed"
	} else if outcome != "succeeded" {
		return errors.New("invalid audit outcome")
	}
	details, err := PluginAuditDetails(action, plugin, "", reason)
	if err != nil {
		return err
	}
	result, err := r.database.ExecContext(ctx, `INSERT INTO management_audit_events(operation_id,actor_kind,actor_user_id,action,resource_type,resource_id,phase,outcome,request_id,details_json) SELECT operation_id,actor_kind,actor_user_id,action,resource_type,resource_id,'completed',?,request_id,? FROM management_audit_events WHERE operation_id=? AND phase='requested'`, outcome, []byte(details.encoded), operation)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("missing requested audit")
	}
	return err
}

package user

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

type AuditEvent struct {
	ID           uint64          `json:"id"`
	OperationID  string          `json:"operation_id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	ActorKind    string          `json:"actor_kind"`
	ActorUserID  *uint64         `json:"actor_user_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Phase        string          `json:"phase"`
	Outcome      string          `json:"outcome"`
	RequestID    string          `json:"request_id"`
	Details      json.RawMessage `json:"details_json"`
}
type AuditQuery struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Action   string    `json:"action"`
	Resource string    `json:"resource"`
	Outcome  string    `json:"outcome"`
	Before   uint64    `json:"before"`
	Limit    int       `json:"limit"`
}

func (r *MySQLRepository) AuditEvents(ctx context.Context, q AuditQuery) ([]AuditEvent, error) {
	query := `SELECT id,operation_id,occurred_at,actor_kind,actor_user_id,action,resource_type,resource_id,phase,outcome,request_id,details_json FROM management_audit_events WHERE occurred_at>=? AND occurred_at<=?`
	args := []any{q.Start, q.End}
	if q.Before != 0 {
		query += ` AND id<?`
		args = append(args, q.Before)
	}
	for _, f := range []struct{ column, value string }{{"action", q.Action}, {"resource_type", q.Resource}, {"outcome", q.Outcome}} {
		if f.value != "" {
			query += " AND " + f.column + "=?"
			args = append(args, f.value)
		}
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := r.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if err = rows.Scan(&e.ID, &e.OperationID, &e.OccurredAt, &e.ActorKind, &e.ActorUserID, &e.Action, &e.ResourceType, &e.ResourceID, &e.Phase, &e.Outcome, &e.RequestID, &e.Details); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// Fixed vocabularies also define the extension boundary for later Phase 15 batches.
func ValidAuditAction(s string) bool {
	switch s {
	case "bootstrap.declare", "user.role.change", "rule.create", "rule.update", "rule.enable", "rule.disable", "rule.delete", "plugin.connection-test", "plugin.install", "plugin.configuration", "plugin.start", "plugin.stop", "plugin.update", "alert.trigger", "alert.recover", "alert.close":
		return true
	}
	return false
}

// RuleAuditDetails and PluginAuditDetails define bounded extension points, not
// synthetic records: future callers still have to execute and audit the action.
func RuleAuditDetails(action string, id, revision uint64, severity, source string) (AuditDetails, error) {
	if !strings.HasPrefix(action, "rule.") || !ValidAuditAction(action) || id == 0 || revision == 0 || (severity != "warning" && severity != "critical") || (source != "metrics" && source != "logs" && source != "events") {
		return AuditDetails{}, errors.New("invalid rule audit details")
	}
	b, err := json.Marshal(struct {
		ID       uint64 `json:"rule_id"`
		Revision uint64 `json:"revision"`
		Severity string `json:"severity"`
		Source   string `json:"source"`
	}{id, revision, severity, source})
	return AuditDetails{b}, err
}
func PluginAuditDetails(action, plugin, version, reason string) (AuditDetails, error) {
	if !strings.HasPrefix(action, "plugin.") || !ValidAuditAction(action) {
		return AuditDetails{}, errors.New("invalid plugin audit action")
	}
	switch plugin {
	case "redis-exporter", "mysql-exporter", "kafka-exporter", "elasticsearch-exporter", "rabbitmq-exporter", "victoriametrics-exporter":
	default:
		return AuditDetails{}, errors.New("invalid plugin audit resource")
	}
	if version != "" && !auditVersion.MatchString(version) {
		return AuditDetails{}, errors.New("invalid plugin audit version")
	}
	switch reason {
	case "", "validation_failed", "operation_failed", "unavailable", "conflict":
	default:
		return AuditDetails{}, errors.New("invalid plugin audit reason")
	}
	b, err := json.Marshal(struct {
		Plugin  string `json:"plugin_id"`
		Version string `json:"version,omitempty"`
		Reason  string `json:"reason_code,omitempty"`
	}{plugin, version, reason})
	return AuditDetails{b}, err
}

var auditVersion = regexp.MustCompile(`^[0-9]{1,9}\.[0-9]{1,9}\.[0-9]{1,9}$`)

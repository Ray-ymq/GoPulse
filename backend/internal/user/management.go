package user

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
)

var ErrBootstrapProtected = apperror.New(apperror.CodeBootstrapProtected, "bootstrap super administrator is protected")
var ErrSetupUnavailable = apperror.New(apperror.CodeSetupUnavailable, "management setup is unavailable")

// Managed exposes only the explicitly authorized user-management contract.
type Managed struct {
	Public
	IsBootstrapSuperAdmin bool `json:"is_bootstrap_super_admin"`
}
type RoleChange struct {
	User    Managed `json:"user"`
	Changed bool    `json:"changed"`
}

func (r *MySQLRepository) BootstrapID(ctx context.Context) (uint64, error) {
	var id uint64
	err := r.database.QueryRowContext(ctx, `SELECT b.user_id FROM bootstrap_super_admin b JOIN users u ON u.id=b.user_id WHERE b.singleton=1 AND u.role='super_admin'`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSetupUnavailable
	}
	return id, err
}

// ValidateBootstrap allows an uninitialized installation, but rejects a corrupt root.
func (r *MySQLRepository) ValidateBootstrap(ctx context.Context) error {
	var role string
	err := r.database.QueryRowContext(ctx, `SELECT u.role FROM bootstrap_super_admin b JOIN users u ON u.id=b.user_id WHERE b.singleton=1`).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if role != string(RoleSuperAdmin) {
		return ErrSetupUnavailable
	}
	return nil
}

func (r *MySQLRepository) ManagedByID(ctx context.Context, id uint64) (Managed, error) {
	record, err := r.FindByID(ctx, id)
	if err != nil {
		return Managed{}, err
	}
	root, err := r.BootstrapID(ctx)
	return Managed{Public: record.Public(), IsBootstrapSuperAdmin: root == id}, err
}

// DeclareBootstrap serializes first declaration on the singleton key. The role,
// singleton and successful audit commit together; a second account cannot replace it.
func (r *MySQLRepository) DeclareBootstrap(ctx context.Context, id uint64) (User, error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var root uint64
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM bootstrap_super_admin WHERE singleton=1 FOR UPDATE`).Scan(&root)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}
	if root != 0 && root != id {
		return User{}, ErrBootstrapProtected
	}
	var role string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=? FOR UPDATE`, id).Scan(&role); errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	} else if err != nil {
		return User{}, err
	}
	if root == id {
		if role != string(RoleSuperAdmin) {
			return User{}, ErrSetupUnavailable
		}
	} else {
		if _, err = ParseRole(role); err != nil {
			return User{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE users SET role='super_admin' WHERE id=?`, id); err != nil {
			return User{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO bootstrap_super_admin(singleton,user_id) VALUES(1,?)`, id); err != nil {
			return User{}, err
		}
		detail, _ := RoleAuditDetails("bootstrap.declare", Role(role), RoleSuperAdmin)
		if err = appendRoleAudit(ctx, tx, 0, id, "bootstrap.declare", "", detail); err != nil {
			return User{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return r.FindByID(ctx, id)
}

func (r *MySQLRepository) ChangeRole(ctx context.Context, actor, target uint64, next Role, requestID string) (RoleChange, error) {
	if _, err := ParseRole(string(next)); err != nil {
		return RoleChange{}, apperror.New(apperror.CodeValidationFailed, "invalid role")
	}
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return RoleChange{}, err
	}
	defer tx.Rollback()
	var actorRole string
	if err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=? FOR UPDATE`, actor).Scan(&actorRole); err != nil {
		return RoleChange{}, err
	}
	if actorRole != string(RoleSuperAdmin) {
		return RoleChange{}, apperror.New(apperror.CodePermissionDenied, "super administrator permission is required")
	}
	var record User
	var before string
	err = tx.QueryRowContext(ctx, `SELECT id,username,role,created_at FROM users WHERE id=? FOR UPDATE`, target).Scan(&record.ID, &record.Username, &before, &record.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RoleChange{}, ErrNotFound
	}
	if err != nil {
		return RoleChange{}, err
	}
	record.Role, err = ParseRole(before)
	if err != nil {
		return RoleChange{}, err
	}
	var root uint64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM bootstrap_super_admin WHERE singleton=1 FOR UPDATE`).Scan(&root); errors.Is(err, sql.ErrNoRows) {
		return RoleChange{}, ErrSetupUnavailable
	} else if err != nil {
		return RoleChange{}, err
	}
	if root == target && next != RoleSuperAdmin {
		return RoleChange{}, ErrBootstrapProtected
	}
	changed := record.Role != next
	if changed {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET role=? WHERE id=?`, next, target); err != nil {
			return RoleChange{}, err
		}
		detail, _ := RoleAuditDetails("user.role.change", record.Role, next)
		if err = appendRoleAudit(ctx, tx, actor, target, "user.role.change", requestID, detail); err != nil {
			return RoleChange{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return RoleChange{}, err
	}
	record.Role = next
	return RoleChange{User: Managed{Public: record.Public(), IsBootstrapSuperAdmin: root == target}, Changed: changed}, nil
}

// AuditDetails has no public fields: callers can only obtain vetted, typed details.
type AuditDetails struct{ encoded json.RawMessage }

func RoleAuditDetails(action string, before, after Role) (AuditDetails, error) {
	if action != "bootstrap.declare" && action != "user.role.change" {
		return AuditDetails{}, errors.New("invalid role audit action")
	}
	if _, err := ParseRole(string(before)); err != nil {
		return AuditDetails{}, err
	}
	if _, err := ParseRole(string(after)); err != nil {
		return AuditDetails{}, err
	}
	b, err := json.Marshal(struct {
		Before Role `json:"before"`
		After  Role `json:"after"`
	}{before, after})
	return AuditDetails{b}, err
}
func appendRoleAudit(ctx context.Context, tx *sql.Tx, actor, target uint64, action, requestID string, details AuditDetails) error {
	var operation [16]byte
	if _, err := rand.Read(operation[:]); err != nil {
		return err
	}
	kind := "user"
	var actorID any = actor
	if actor == 0 {
		kind = "system"
		actorID = nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO management_audit_events(operation_id,actor_kind,actor_user_id,action,resource_type,resource_id,phase,outcome,request_id,details_json) VALUES(?,?,?,?,'user',?,'completed','succeeded',?,?)`, hex.EncodeToString(operation[:]), kind, actorID, action, target, requestID, []byte(details.encoded))
	return err
}

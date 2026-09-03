package user

import (
	"fmt"
	"time"
)

// Role is the persisted server-authoritative authorization role.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

func ParseRole(value string) (Role, error) {
	role := Role(value)
	if role != RoleUser && role != RoleAdmin {
		return "", fmt.Errorf("invalid user role")
	}
	return role, nil
}

// User is the persisted authentication record. PasswordHash is intentionally
// excluded from JSON so it cannot enter a public response by accident.
type User struct {
	ID           uint64    `json:"-"`
	Username     string    `json:"-"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"-"`
	CreatedAt    time.Time `json:"-"`
}

// Public is the current-user representation exposed by authentication APIs.
type Public struct {
	ID        uint64    `json:"id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func (record User) Public() Public {
	return Public{
		ID:        record.ID,
		Username:  record.Username,
		Role:      record.Role,
		CreatedAt: record.CreatedAt,
	}
}

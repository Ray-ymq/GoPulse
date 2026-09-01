package user

import "time"

// User is the persisted authentication record. PasswordHash is intentionally
// excluded from JSON so it cannot enter a public response by accident.
type User struct {
	ID           uint64    `json:"-"`
	Username     string    `json:"-"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"-"`
}

// Public is the only user representation exposed through the HTTP API.
type Public struct {
	ID        uint64    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

func (record User) Public() Public {
	return Public{
		ID:        record.ID,
		Username:  record.Username,
		CreatedAt: record.CreatedAt,
	}
}

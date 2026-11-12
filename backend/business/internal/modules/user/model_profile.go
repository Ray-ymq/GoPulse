package user

import (
	"time"
)

// Profile deliberately excludes authentication and authorization fields.
type Profile struct {
	ID          uint64    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	CreatedAt   time.Time `json:"created_at"`
	Following   bool      `json:"following"`
	IsSelf      bool      `json:"is_self"`
}

type ProfileInput struct {
	DisplayName *string `json:"display_name"`
	Bio         *string `json:"bio"`
}

type UserSearchOptions struct {
	Query string
	Limit int
	after *userSearchCursor
}

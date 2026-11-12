package post

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("post not found")

type Repository interface {
	Create(context.Context, uint64, string, string) (Post, error)
	List(context.Context, uint64, ListOptions) ([]Post, error)
	FindPublicByID(context.Context, uint64) (PublicProjection, error)
	LikedByViewer(context.Context, uint64, uint64) (bool, error)
	Exists(context.Context, uint64) (bool, error)
}

// RowQueryer is the small query boundary required by transactional fact
// writers. It deliberately accepts both *sql.DB and *sql.Tx without exposing
// transaction handles to HTTP handlers.
type RowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

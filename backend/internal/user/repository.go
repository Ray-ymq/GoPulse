package user

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound         = errors.New("user not found")
	ErrUsernameConflict = errors.New("username conflict")
)

type Repository interface {
	Create(context.Context, string, string) (User, error)
	FindByID(context.Context, uint64) (User, error)
	FindByUsername(context.Context, string) (User, error)
}

type MySQLRepository struct {
	database *sql.DB
}

func NewMySQLRepository(database *sql.DB) *MySQLRepository {
	return &MySQLRepository{database: database}
}

func (repository *MySQLRepository) Create(ctx context.Context, username, passwordHash string) (User, error) {
	result, err := repository.database.ExecContext(ctx,
		`INSERT INTO users (username, password_hash) VALUES (?, ?)`,
		username,
		passwordHash,
	)
	if err != nil {
		if isDuplicateEntry(err) {
			return User{}, ErrUsernameConflict
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}

	identifier, err := result.LastInsertId()
	if err != nil || identifier <= 0 {
		return User{}, errors.New("create user: invalid inserted identifier")
	}
	return repository.FindByID(ctx, uint64(identifier))
}

func (repository *MySQLRepository) FindByID(ctx context.Context, identifier uint64) (User, error) {
	return repository.findOne(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE id = ?`,
		identifier,
	)
}

func (repository *MySQLRepository) FindByUsername(ctx context.Context, normalizedUsername string) (User, error) {
	return repository.findOne(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`,
		normalizedUsername,
	)
}

func (repository *MySQLRepository) findOne(ctx context.Context, query string, argument any) (User, error) {
	var record User
	err := repository.database.QueryRowContext(ctx, query, argument).Scan(
		&record.ID,
		&record.Username,
		&record.PasswordHash,
		&record.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return record, nil
}

func isDuplicateEntry(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == 1062
}

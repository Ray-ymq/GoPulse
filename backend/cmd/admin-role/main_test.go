package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

type fakeRolePromoter struct {
	username string
	record   user.User
	err      error
}

func (promoter *fakeRolePromoter) PromoteByUsername(_ context.Context, username string) (user.User, error) {
	promoter.username = username
	return promoter.record, promoter.err
}

func TestRunPromotesNormalizedUsernameAndIsIdempotent(t *testing.T) {
	for _, initialRole := range []user.Role{user.RoleUser, user.RoleAdmin} {
		promoter := &fakeRolePromoter{record: user.User{ID: 17, Role: user.RoleAdmin}}
		var output bytes.Buffer
		err := run([]string{"promote", "--username", " Alice "}, &output, func() (rolePromoter, func(), error) {
			return promoter, func() {}, nil
		})
		if err != nil {
			t.Fatalf("run() role=%q error = %v", initialRole, err)
		}
		if promoter.username != "Alice" {
			t.Fatalf("promoted username = %q, want Alice", promoter.username)
		}
		if output.String() != "administrator role ensured\n" {
			t.Fatalf("output = %q", output.String())
		}
	}
}

func TestRunRejectsInvalidArgumentsBeforeOpeningStorage(t *testing.T) {
	for _, args := range [][]string{nil, {"demote", "--username", "alice"}, {"promote"}, {"promote", "--username", "a!"}, {"promote", "--username", "alice", "extra"}} {
		opened := false
		err := run(args, &bytes.Buffer{}, func() (rolePromoter, func(), error) {
			opened = true
			return nil, func() {}, nil
		})
		if err == nil {
			t.Fatalf("run(%v) error = nil", args)
		}
		if opened {
			t.Fatalf("run(%v) opened storage", args)
		}
	}
}

func TestRunMapsNotFoundAndStorageFailuresToSafeErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		openError  error
		promoteErr error
		want       string
	}{
		{name: "open", openError: errors.New("secret dsn"), want: "initialize administrator role storage"},
		{name: "not found", promoteErr: user.ErrNotFound, want: "registered user was not found"},
		{name: "database", promoteErr: errors.New("sql with password"), want: "promote administrator role"},
	} {
		t.Run(test.name, func(t *testing.T) {
			promoter := &fakeRolePromoter{record: user.User{Role: user.RoleAdmin}, err: test.promoteErr}
			err := run([]string{"promote", "--username", "alice"}, &bytes.Buffer{}, func() (rolePromoter, func(), error) {
				if test.openError != nil {
					return nil, func() {}, test.openError
				}
				return promoter, func() {}, nil
			})
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			for _, secret := range []string{"secret dsn", "sql with password"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaked %q: %v", secret, err)
				}
			}
		})
	}
}

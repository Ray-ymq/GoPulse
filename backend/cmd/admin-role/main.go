package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

const operationTimeout = 5 * time.Second

type rolePromoter interface {
	PromoteByUsername(context.Context, string) (user.User, error)
}

type openPromoterFunc func() (rolePromoter, func(), error)

func main() {
	if err := run(os.Args[1:], os.Stdout, openPromoter); err != nil {
		log.Printf("super administrator role command failed: %v", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer, open openPromoterFunc) error {
	if len(args) == 3 && args[0] == "bootstrap" && args[1] == "--user-id" {
		id, err := strconv.ParseUint(args[2], 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != args[2] {
			return errors.New("user ID must be a canonical positive integer")
		}
		promoter, closePromoter, err := open()
		if err != nil {
			return errors.New("initialize bootstrap storage")
		}
		defer closePromoter()
		declarer, ok := promoter.(interface {
			DeclareBootstrap(context.Context, uint64) (user.User, error)
		})
		if !ok {
			return errors.New("bootstrap declaration unavailable")
		}
		ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
		defer cancel()
		if _, err = declarer.DeclareBootstrap(ctx, id); err != nil {
			return errors.New("bootstrap declaration rejected")
		}
		_, _ = fmt.Fprintln(output, "bootstrap super administrator ensured")
		return nil
	}
	if len(args) == 0 || args[0] != "promote" {
		return errors.New("usage: admin-role bootstrap --user-id <id> | promote --username <username>")
	}
	flags := flag.NewFlagSet("promote", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var usernameValue string
	flags.StringVar(&usernameValue, "username", "", "registered username to promote")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("usage: admin-role bootstrap --user-id <id> | promote --username <username>")
	}
	username, err := user.NormalizeUsername(usernameValue)
	if err != nil {
		return errors.New("username must match [A-Za-z0-9_]{3,32}")
	}

	promoter, closePromoter, err := open()
	if err != nil {
		return errors.New("initialize super administrator role storage")
	}
	defer closePromoter()

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	record, err := promoter.PromoteByUsername(ctx, username)
	if errors.Is(err, user.ErrNotFound) {
		return errors.New("registered user was not found")
	}
	if err != nil || record.Role != user.RoleSuperAdmin {
		return errors.New("promote super administrator role")
	}
	_, _ = fmt.Fprintln(output, "super administrator role ensured")
	return nil
}

func openPromoter() (rolePromoter, func(), error) {
	mysqlConfig, err := config.LoadMySQL()
	if err != nil {
		return nil, func() {}, err
	}
	database, err := platform.OpenMySQLDatabase(mysqlConfig)
	if err != nil {
		return nil, func() {}, err
	}
	return user.NewMySQLRepository(database), func() { _ = database.Close() }, nil
}

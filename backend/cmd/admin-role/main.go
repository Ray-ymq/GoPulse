package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
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
		log.Printf("administrator role command failed: %v", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer, open openPromoterFunc) error {
	if len(args) == 0 || args[0] != "promote" {
		return errors.New("usage: admin-role promote --username <username>")
	}
	flags := flag.NewFlagSet("promote", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var usernameValue string
	flags.StringVar(&usernameValue, "username", "", "registered username to promote")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("usage: admin-role promote --username <username>")
	}
	username, err := user.NormalizeUsername(usernameValue)
	if err != nil {
		return errors.New("username must match [A-Za-z0-9_]{3,32}")
	}

	promoter, closePromoter, err := open()
	if err != nil {
		return errors.New("initialize administrator role storage")
	}
	defer closePromoter()

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	record, err := promoter.PromoteByUsername(ctx, username)
	if errors.Is(err, user.ErrNotFound) {
		return errors.New("registered user was not found")
	}
	if err != nil || record.Role != user.RoleAdmin {
		return errors.New("promote administrator role")
	}
	_, _ = fmt.Fprintln(output, "administrator role ensured")
	return nil
}

func openPromoter() (rolePromoter, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, func() {}, err
	}
	database, err := platform.OpenMySQLDatabase(cfg.MySQL)
	if err != nil {
		return nil, func() {}, err
	}
	return user.NewMySQLRepository(database), func() { _ = database.Close() }, nil
}

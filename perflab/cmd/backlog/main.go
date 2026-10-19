// Command backlog exposes the pending business outbox count as a small HTTP
// JSON endpoint. The k6 orchestrator uses it as the recovery probe: a stage is
// only considered recovered when the asynchronous backlog returns to the
// baseline measured before the stage.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// PendingCounter reports the number of business outbox rows that have not been
// published yet.
type PendingCounter interface {
	Pending(ctx context.Context) (int64, error)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	flags := flag.NewFlagSet("gopulse-backlog", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var dsnFile, listen string
	flags.StringVar(&dsnFile, "dsn-file", "", "private owner-only file containing the MySQL DSN")
	flags.StringVar(&listen, "listen", "127.0.0.1:18081", "loopback address for the backlog endpoint")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || dsnFile == "" {
		fmt.Fprintln(os.Stderr, "backlog requires --dsn-file")
		return 2
	}
	if err := validateLoopbackListen(listen); err != nil {
		fmt.Fprintln(os.Stderr, "backlog listen address must be loopback:", err)
		return 2
	}
	dsn, err := readPrivateValue(dsnFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read private backlog DSN failed")
		return 2
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open backlog database failed")
		return 1
	}
	defer func() { _ = database.Close() }()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(3 * time.Minute)

	listener, err := net.Listen("tcp", listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen on backlog address failed")
		return 1
	}
	server := &http.Server{
		Handler:           newHandler(&mysqlCounter{database: database}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "serve backlog endpoint failed")
		return 1
	}
	return 0
}

// mysqlCounter counts outbox rows that are not in the published terminal
// state. Rows are inserted as pending and become published after the worker
// delivers them, so this value is the asynchronous work still owed.
type mysqlCounter struct {
	database *sql.DB
}

func (counter *mysqlCounter) Pending(ctx context.Context) (int64, error) {
	var pending int64
	err := counter.database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM business_outbox WHERE status <> 'published'`).Scan(&pending)
	if err != nil {
		return 0, err
	}
	return pending, nil
}

func newHandler(counter PendingCounter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/backlog", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		pending, err := counter.Pending(ctx)
		if err != nil {
			http.Error(writer, "backlog unavailable", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(writer).Encode(map[string]int64{"pending": pending})
	})
	return mux
}

func validateLoopbackListen(value string) error {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return err
	}
	if host == "" {
		return errors.New("host must be explicit loopback")
	}
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() {
		return errors.New("host must be a loopback IP")
	}
	return nil
}

// readPrivateValue mirrors the recipe contract: the value must live in an
// owner-only regular file, never in the process arguments or Git.
func readPrivateValue(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 8192 || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("private value file is invalid")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(encoded))
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", errors.New("private value is empty or invalid")
	}
	return value, nil
}

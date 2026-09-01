package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

const (
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 1 << 20
)

func main() {
	if err := run(); err != nil {
		log.Printf("backend stopped with error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if err := backendhttp.ConfigureGinMode(cfg.AppEnv); err != nil {
		return fmt.Errorf("configure Gin mode: %w", err)
	}

	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return errors.New("initialize MySQL client")
	}
	defer closeResource("MySQL", mysqlClient.Close)

	redisClient := platform.NewRedis(cfg.Redis)
	defer closeResource("Redis", redisClient.Close)

	rabbitMQChecker, err := platform.NewRabbitMQ(cfg.RabbitMQURL)
	if err != nil {
		return errors.New("initialize RabbitMQ checker")
	}

	passwords, err := auth.NewPasswordManager()
	if err != nil {
		return errors.New("initialize password manager")
	}
	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		return errors.New("initialize token manager")
	}
	cookies := auth.NewCookieManager(cfg.Auth.CookieName, cfg.Auth.CookieSecure, cfg.Auth.JWTTTL, time.Now)
	users := user.NewMySQLRepository(mysqlClient.DB())
	authService := auth.NewService(users, passwords, tokens)
	authHandler := auth.NewHandler(authService, cookies)
	posts := post.NewMySQLRepository(mysqlClient.DB())
	postService := post.NewService(posts)
	postHandler := post.NewHandler(postService)
	comments := comment.NewMySQLRepository(mysqlClient.DB())
	commentService := comment.NewService(comments, postService)
	commentHandler := comment.NewHandler(commentService)
	likes := like.NewMySQLRepository(mysqlClient.DB())
	likeService := like.NewService(likes, postService)
	likeHandler := like.NewHandler(likeService)

	router := backendhttp.NewRouter(
		backendhttp.Dependencies{
			MySQL:    mysqlClient,
			Redis:    redisClient,
			RabbitMQ: rabbitMQChecker,
		},
		backendhttp.APIRoutes{
			Auth:           authHandler,
			Posts:          postHandler,
			Comments:       commentHandler,
			Likes:          likeHandler,
			Authentication: middleware.RequireAuthentication(cookies.Name(), tokens),
		},
	)
	server := newHTTPServer(cfg.HTTPAddress(), router)

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	return serve(signalContext, server, server.ListenAndServe)
}

func newHTTPServer(address string, handler stdhttp.Handler) *stdhttp.Server {
	return &stdhttp.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

func serve(ctx context.Context, server *stdhttp.Server, startServer func() error) error {
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- startServer()
	}()

	log.Printf("backend listening on %s", server.Addr)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("HTTP server shutdown failed: %w", err)
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed during shutdown: %w", err)
		}
		return nil
	}
}

func closeResource(name string, close func() error) {
	if err := close(); err != nil {
		log.Printf("%s resource close failed", name)
	}
}

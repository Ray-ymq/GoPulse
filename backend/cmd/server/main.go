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
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	searchpkg "github.com/Ray-ymq/GoPulse/backend/internal/search"
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

	elasticsearchClient, err := platform.NewElasticsearch(cfg.Elasticsearch)
	if err != nil {
		return errors.New("initialize Elasticsearch client")
	}

	rabbitMQChecker, err := platform.NewRabbitMQ(cfg.RabbitMQURL)
	if err != nil {
		return errors.New("initialize RabbitMQ checker")
	}

	eventOutbox, err := outbox.NewRepository(mysqlClient.DB(), outbox.Options{
		MaxClaimBatch: cfg.Outbox.ClaimBatch,
	})
	if err != nil {
		return errors.New("initialize business outbox repository")
	}
	rabbitMQPublisher, err := platform.NewRabbitMQPublisher(
		cfg.RabbitMQURL,
		platform.RabbitMQPublisherOptions{RetryDelay: cfg.Outbox.RetryDelay, SearchRetryDelay: cfg.Outbox.SearchRetryDelay},
	)
	if err != nil {
		return errors.New("initialize RabbitMQ publisher")
	}
	defer closeResource("RabbitMQ publisher", rabbitMQPublisher.Close)

	dispatcher, err := outbox.NewDispatcher(eventOutbox, rabbitMQPublisher, outbox.DispatcherOptions{
		PollInterval:    cfg.Outbox.PollInterval,
		ClaimBatch:      cfg.Outbox.ClaimBatch,
		LeaseDuration:   cfg.Outbox.LeaseDuration,
		PublishTimeout:  cfg.Outbox.PublishTimeout,
		CleanupInterval: cfg.Outbox.CleanupInterval,
		Retention:       cfg.Outbox.Retention,
		CleanupBatch:    cfg.Outbox.CleanupBatch,
	})
	if err != nil {
		return errors.New("initialize outbox dispatcher")
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
	postDetailCache := rediscache.NewPostDetailRepository(
		redisClient,
		cfg.Redis.PostDetailTTL,
		cfg.Redis.OperationTimeout,
	)
	posts := post.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
	postService := post.NewService(posts, postDetailCache)
	postHandler := post.NewHandler(postService)
	comments := comment.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
	commentService := comment.NewService(comments, postService, postDetailCache)
	commentHandler := comment.NewHandler(commentService)
	likes := like.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
	likeService := like.NewService(likes, postService, postDetailCache)
	likeHandler := like.NewHandler(likeService)
	notifications, err := notification.NewRepository(mysqlClient.DB())
	if err != nil {
		return errors.New("initialize notification repository")
	}
	notificationService := notification.NewService(notifications)
	notificationHandler := notification.NewHandler(notificationService)
	searchRepository := searchpkg.NewElasticsearchRepository(elasticsearchClient)
	searchService := searchpkg.NewService(searchRepository, posts)
	searchHandler := searchpkg.NewHandler(searchService)

	router := backendhttp.NewRouter(
		backendhttp.Dependencies{
			MySQL:         mysqlClient,
			Redis:         redisClient,
			RabbitMQ:      rabbitMQChecker,
			Elasticsearch: elasticsearchClient,
		},
		backendhttp.APIRoutes{
			Auth:           authHandler,
			Posts:          postHandler,
			Comments:       commentHandler,
			Likes:          likeHandler,
			Notifications:  notificationHandler,
			Search:         searchHandler,
			Authentication: middleware.RequireAuthentication(cookies.Name(), tokens),
		},
	)
	server := newHTTPServer(cfg.HTTPAddress(), router)

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	return serveWithDispatcher(signalContext, server, dispatcher)
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

func serveWithDispatcher(ctx context.Context, server *stdhttp.Server, dispatcher *outbox.Dispatcher) error {
	if ctx == nil {
		return errors.New("serve backend: context is required")
	}
	if server == nil {
		return errors.New("serve backend: HTTP server is required")
	}
	if dispatcher == nil {
		return errors.New("serve backend: outbox dispatcher is required")
	}

	dispatcherContext, cancelDispatcher := context.WithCancel(ctx)
	dispatcherErrors := make(chan error, 1)
	go func() {
		dispatcherErrors <- dispatcher.Run(dispatcherContext)
	}()

	serverErr := serve(ctx, server, server.ListenAndServe)
	cancelDispatcher()

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	select {
	case dispatcherErr := <-dispatcherErrors:
		if dispatcherErr != nil && !errors.Is(dispatcherErr, context.Canceled) {
			if serverErr != nil {
				return fmt.Errorf("%v; outbox dispatcher shutdown failed: %w", serverErr, dispatcherErr)
			}
			return fmt.Errorf("outbox dispatcher shutdown failed: %w", dispatcherErr)
		}
	case <-shutdownContext.Done():
		if serverErr != nil {
			return fmt.Errorf("%v; outbox dispatcher shutdown timed out", serverErr)
		}
		return errors.New("outbox dispatcher shutdown timed out")
	}
	return serverErr
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

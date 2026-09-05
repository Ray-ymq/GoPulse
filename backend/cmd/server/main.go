package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logship"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	searchpkg "github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	goredis "github.com/redis/go-redis/v9"
	redislogging "github.com/redis/go-redis/v9/logging"
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
	stdoutLogger := logging.New("backend", os.Stdout)
	cfg, err := config.Load()
	if err != nil {
		logging.Module(stdoutLogger, "lifecycle").Error("backend stopped", slog.String("reason", "invalid_configuration"))
		os.Exit(1)
	}
	logger := stdoutLogger
	var shipper *logship.Shipper
	if cfg.LogShip.Enabled() {
		shipper, err = logship.New(logship.Config{
			Endpoint: cfg.LogShip.Endpoint, Token: cfg.LogShip.Token, RequestTimeout: cfg.LogShip.RequestTimeout,
			QueueCapacity: cfg.LogShip.QueueCapacity, RetryMin: cfg.LogShip.RetryMin, RetryMax: cfg.LogShip.RetryMax,
			ShutdownTimeout: cfg.LogShip.ShutdownTimeout,
		}, logging.Module(stdoutLogger, "logship"))
		if err != nil {
			logging.Module(stdoutLogger, "lifecycle").Error("backend stopped", slog.String("reason", "invalid_log_shipper"))
			os.Exit(1)
		}
		logger = logging.NewWithSink("backend", os.Stdout, shipper)
	}
	if err = run(cfg, logger); err != nil {
		logging.Module(logger, "lifecycle").Error("backend stopped", slog.String("reason", "process_failed"))
		if shipper != nil {
			ctx, cancel := context.WithTimeout(context.Background(), cfg.LogShip.ShutdownTimeout)
			_ = shipper.Close(ctx)
			cancel()
		}
		os.Exit(1)
	}
	if shipper != nil {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.LogShip.ShutdownTimeout)
		if err := shipper.Close(ctx); err != nil {
			logging.Module(stdoutLogger, "logship").Warn("log shipper shutdown incomplete", slog.String("reason", "shutdown_timeout"))
		}
		cancel()
	}
}

func run(cfg config.Config, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Discard("backend")
	}
	lifecycleLogger := logging.Module(logger, "lifecycle")
	goredis.SetLogger(&redislogging.VoidLogger{})
	if err := backendhttp.ConfigureGinMode(cfg.AppEnv); err != nil {
		return fmt.Errorf("configure Gin mode: %w", err)
	}

	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return errors.New("initialize MySQL client")
	}
	defer closeResource(lifecycleLogger, "mysql", mysqlClient.Close)

	redisClient := platform.NewRedis(cfg.Redis)
	defer closeResource(lifecycleLogger, "redis", redisClient.Close)

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
	defer closeResource(lifecycleLogger, "rabbitmq_publisher", rabbitMQPublisher.Close)

	dispatcher, err := outbox.NewDispatcher(eventOutbox, rabbitMQPublisher, outbox.DispatcherOptions{
		PollInterval:    cfg.Outbox.PollInterval,
		ClaimBatch:      cfg.Outbox.ClaimBatch,
		LeaseDuration:   cfg.Outbox.LeaseDuration,
		PublishTimeout:  cfg.Outbox.PublishTimeout,
		CleanupInterval: cfg.Outbox.CleanupInterval,
		Retention:       cfg.Outbox.Retention,
		CleanupBatch:    cfg.Outbox.CleanupBatch,
		Logger:          logging.Module(logger, "outbox"),
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
	authHandler := auth.NewHandler(authService, cookies, logger)
	postDetailCache := rediscache.NewPostDetailRepository(
		redisClient,
		cfg.Redis.PostDetailTTL,
		cfg.Redis.OperationTimeout,
	)
	posts := post.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
	postService := post.NewService(posts, postDetailCache).WithLogger(logger)
	postHandler := post.NewHandler(postService, logger)
	comments := comment.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
	commentService := comment.NewService(comments, postService, postDetailCache).WithLogger(logger)
	commentHandler := comment.NewHandler(commentService, logger)
	likes := like.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
	likeService := like.NewService(likes, postService, postDetailCache).WithLogger(logger)
	likeHandler := like.NewHandler(likeService, logger)
	notifications, err := notification.NewRepository(mysqlClient.DB())
	if err != nil {
		return errors.New("initialize notification repository")
	}
	notificationService := notification.NewService(notifications)
	notificationHandler := notification.NewHandler(notificationService, logger)
	searchRepository := searchpkg.NewElasticsearchRepository(elasticsearchClient)
	searchService := searchpkg.NewService(searchRepository, posts, cfg.Auth.JWTSecret)
	searchHandler := searchpkg.NewHandler(searchService)
	logRepository := logquery.NewElasticsearchRepository(elasticsearchClient)
	logService := logquery.NewService(logRepository, cfg.Auth.JWTSecret)
	logHandler := logquery.NewHandler(logService)
	eventRepository := eventquery.NewElasticsearchRepository(elasticsearchClient)
	eventService := eventquery.NewService(eventRepository, cfg.Auth.JWTSecret)
	eventHandler := eventquery.NewHandler(eventService)
	monitorClient, err := exporterplugin.NewClient(cfg.Monitor.URL, cfg.Monitor.APIToken, cfg.Monitor.RequestTimeout)
	if err != nil {
		return errors.New("initialize monitor client")
	}
	exporterPluginHandler := exporterplugin.NewHandler(monitorClient)

	router := backendhttp.NewRouter(
		backendhttp.Dependencies{
			MySQL:         mysqlClient,
			Redis:         redisClient,
			RabbitMQ:      rabbitMQChecker,
			Elasticsearch: elasticsearchClient,
			Logger:        logger,
		},
		backendhttp.APIRoutes{
			Auth:            authHandler,
			Posts:           postHandler,
			Comments:        commentHandler,
			Likes:           likeHandler,
			Logs:            logHandler,
			Events:          eventHandler,
			Notifications:   notificationHandler,
			Search:          searchHandler,
			Authentication:  middleware.RequireAuthentication(cookies.Name(), tokens),
			Authorization:   middleware.RequireAdmin(users),
			ExporterPlugins: exporterPluginHandler,
		},
	)
	server := newHTTPServer(cfg.HTTPAddress(), router)

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	return serveWithDispatcher(signalContext, server, dispatcher, lifecycleLogger)
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

func serveWithDispatcher(ctx context.Context, server *stdhttp.Server, dispatcher *outbox.Dispatcher, logger *slog.Logger) error {
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

	serverErr := serveLogged(ctx, server, server.ListenAndServe, logger)
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
	return serveLogged(ctx, server, startServer, logging.Module(logging.Discard("backend"), "lifecycle"))
}

func serveLogged(ctx context.Context, server *stdhttp.Server, startServer func() error, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.Module(logging.Discard("backend"), "lifecycle")
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- startServer()
	}()

	logger.Info("backend listening")

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			logger.Error("backend server failed", slog.String("reason", "listen_failed"))
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		logger.Info("backend stopped", slog.String("reason", "server_closed"))
		return nil
	case <-ctx.Done():
		logger.Info("backend shutdown started")
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("backend shutdown failed", slog.String("reason", "shutdown_failed"))
			return fmt.Errorf("HTTP server shutdown failed: %w", err)
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			logger.Error("backend shutdown failed", slog.String("reason", "server_failed"))
			return fmt.Errorf("HTTP server failed during shutdown: %w", err)
		}
		logger.Info("backend stopped", slog.String("reason", "shutdown_complete"))
		return nil
	}
}

func closeResource(logger *slog.Logger, resource string, close func() error) {
	if err := close(); err != nil {
		logger.Warn("resource close failed", slog.String("resource", resource), slog.String("reason", "close_failed"))
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"os"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/adminoverview"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert/count"
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/bookmark"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/observability/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	searchpkg "github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
)

type roleProfile struct {
	role       config.ServiceRole
	business   bool
	platform   bool
	dispatcher bool
}

func profileForRole(role config.ServiceRole) (roleProfile, error) {
	if !role.IsValid() {
		return roleProfile{}, errors.New("invalid backend service role")
	}
	return roleProfile{
		role:       role,
		business:   role.RunsBusiness(),
		platform:   role.RunsPlatform(),
		dispatcher: role.RunsBusiness(),
	}, nil
}

type roleResource struct {
	name  string
	close func() error
}

type roleAssembly struct {
	profile       roleProfile
	probes        *componentmetrics.Probes
	router        stdhttp.Handler
	dispatcher    *outbox.Dispatcher
	sampleOutbox  func(context.Context, *componentmetrics.Backend)
	runAlertCheck func(context.Context)
	resources     []roleResource
	closed        bool
}

func (assembly *roleAssembly) addResource(name string, close func() error) {
	if close != nil {
		assembly.resources = append(assembly.resources, roleResource{name: name, close: close})
	}
}

func (assembly *roleAssembly) close(logger *slog.Logger) {
	if assembly == nil || assembly.closed {
		return
	}
	assembly.closed = true
	for index := len(assembly.resources) - 1; index >= 0; index-- {
		resource := assembly.resources[index]
		if err := resource.close(); err != nil && logger != nil {
			logger.Warn("backend resource shutdown incomplete", slog.String("resource", resource.name), slog.String("reason", "close_failed"))
		}
	}
}

func newRoleAssembly(cfg config.Config, logger *slog.Logger, root context.Context) (_ *roleAssembly, err error) {
	profile, err := profileForRole(cfg.ServiceRole)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("assemble backend role: context is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	assembly := &roleAssembly{profile: profile}
	defer func() {
		if err != nil {
			assembly.close(logger)
		}
	}()

	mysqlClient, err := platform.NewMySQL(cfg.MySQL)
	if err != nil {
		return nil, errors.New("initialize MySQL client")
	}
	assembly.addResource("mysql", mysqlClient.Close)

	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		return nil, errors.New("initialize token manager")
	}
	cookies := auth.NewCookieManager(cfg.Auth.CookieName, cfg.Auth.CookieSecure, cfg.Auth.JWTTTL, time.Now)
	users := user.NewMySQLRepository(mysqlClient.DB())
	if err := users.ValidateBootstrap(context.Background()); err != nil {
		return nil, fmt.Errorf("validate management bootstrap: %w", err)
	}
	authentication := middleware.RequireAuthentication(cookies.Name(), tokens)

	var redisClient *platform.Redis
	var elasticsearchClient *platform.Elasticsearch
	var rabbitMQChecker *platform.RabbitMQ
	var eventOutbox *outbox.Repository
	var dispatcher *outbox.Dispatcher
	routes := backendhttp.APIRoutes{Authentication: authentication}
	if profile.business {
		redisClient = platform.NewRedis(cfg.Redis)
		assembly.addResource("redis", redisClient.Close)

		elasticsearchClient, err = platform.NewElasticsearch(cfg.Elasticsearch)
		if err != nil {
			return nil, errors.New("initialize Elasticsearch client")
		}

		rabbitMQChecker, err = platform.NewRabbitMQ(cfg.RabbitMQURL)
		if err != nil {
			return nil, errors.New("initialize RabbitMQ checker")
		}
		eventOutbox, err = outbox.NewRepository(mysqlClient.DB(), outbox.Options{MaxClaimBatch: cfg.Outbox.ClaimBatch})
		if err != nil {
			return nil, errors.New("initialize business outbox repository")
		}
		rabbitMQPublisher, publisherErr := platform.NewRabbitMQPublisher(
			cfg.RabbitMQURL,
			platform.RabbitMQPublisherOptions{RetryDelay: cfg.Outbox.RetryDelay, SearchRetryDelay: cfg.Outbox.SearchRetryDelay},
		)
		if publisherErr != nil {
			return nil, errors.New("initialize RabbitMQ publisher")
		}
		assembly.addResource("rabbitmq_publisher", rabbitMQPublisher.Close)
		dispatcher, err = outbox.NewDispatcher(eventOutbox, rabbitMQPublisher, outbox.DispatcherOptions{
			Owner:           componentmetrics.InstanceID("backend") + "-" + strconv.Itoa(os.Getpid()),
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
			return nil, errors.New("initialize outbox dispatcher")
		}

		passwords, passwordErr := auth.NewPasswordManager()
		if passwordErr != nil {
			return nil, errors.New("initialize password manager")
		}
		authService := auth.NewService(users, passwords, tokens)
		authHandler := auth.NewHandler(authService, cookies, logger)
		postDetailCache := rediscache.NewPostDetailRepository(redisClient, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
		posts := post.NewMySQLRepository(mysqlClient.DB(), post.RepositoryOptions{Outbox: eventOutbox, Logger: logger})
		postService := post.NewService(posts, postDetailCache).WithLogger(logger)
		postHandler := post.NewHandler(postService, logger).WithBookmarkCursorSecret(cfg.Auth.JWTSecret)
		comments := comment.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
		commentService := comment.NewService(comments, postService, postDetailCache).WithLogger(logger)
		commentHandler := comment.NewHandler(commentService, logger)
		likes := like.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
		likeService := like.NewService(likes, postService, postDetailCache).WithLogger(logger)
		likeHandler := like.NewHandler(likeService, logger)
		notifications, notificationErr := notification.NewRepository(mysqlClient.DB())
		if notificationErr != nil {
			return nil, errors.New("initialize notification repository")
		}
		notificationService := notification.NewService(notifications)
		notificationHandler := notification.NewHandler(notificationService, logger)
		searchRepository := searchpkg.NewElasticsearchRepository(elasticsearchClient)
		searchService := searchpkg.NewService(searchRepository, posts, cfg.Auth.JWTSecret)

		routes.Auth = authHandler
		routes.Users = backendhttp.NewUserHandler(user.NewProfileService(users, cfg.Auth.JWTSecret), postService, users)
		routes.Posts = postHandler
		routes.Comments = commentHandler
		routes.Likes = likeHandler
		routes.Bookmarks = bookmark.NewHandler(bookmark.NewService(bookmark.NewMySQLRepository(mysqlClient.DB()), postService), logger)
		routes.Notifications = notificationHandler
		routes.Search = searchpkg.NewHandler(searchService)
		assembly.dispatcher = dispatcher
		assembly.sampleOutbox = func(ctx context.Context, metrics *componentmetrics.Backend) { eventOutbox.SampleMetrics(ctx, metrics) }
	}

	if profile.platform {
		observabilityElasticsearchClient, observationErr := platform.NewObservabilityElasticsearch(cfg.ObservabilityElasticsearch)
		if observationErr != nil {
			return nil, errors.New("initialize observability Elasticsearch client")
		}
		metricClient, metricErr := metricquery.NewClient(cfg.VictoriaMetrics.URL, cfg.VictoriaMetrics.Username, cfg.VictoriaMetrics.Password, cfg.VictoriaMetrics.RequestTimeout)
		if metricErr != nil {
			return nil, errors.New("initialize VictoriaMetrics query client")
		}
		alertRepo := alert.NewRepository(mysqlClient.DB())
		metricHandler := metricquery.NewHandler(metricquery.NewService(metricClient))
		logRepository := logquery.NewElasticsearchRepository(observabilityElasticsearchClient)
		logService := logquery.NewService(logRepository, cfg.Auth.JWTSecret)
		logHandler := logquery.NewHandler(logService)
		eventRepository := eventquery.NewElasticsearchRepository(observabilityElasticsearchClient)
		eventService := eventquery.NewService(eventRepository, cfg.Auth.JWTSecret)
		eventHandler := eventquery.NewHandler(eventService)
		monitorClient, monitorErr := exporterplugin.NewClient(cfg.Monitor.URL, cfg.Monitor.APIToken, cfg.Monitor.RequestTimeout)
		if monitorErr != nil {
			return nil, errors.New("initialize monitor client")
		}
		exporterPluginHandler := exporterplugin.NewHandler(monitorClient).WithAudit(users)
		overview := &adminoverview.Service{
			Components: adminoverview.Components(metricClient), KeyMetrics: adminoverview.KeyMetrics(metricClient), Plugins: adminoverview.Plugins(monitorClient, metricClient),
			Logs: adminoverview.Counts(func(ctx context.Context, severity string, from, to time.Time) (int64, error) {
				return count.Query(ctx, observabilityElasticsearchClient, logquery.ReadAlias, map[string]string{"level": severity}, from, to)
			}),
			Events: adminoverview.Counts(func(ctx context.Context, severity string, from, to time.Time) (int64, error) {
				return count.Query(ctx, observabilityElasticsearchClient, eventquery.ReadAlias, map[string]string{"severity": severity}, from, to)
			}),
			Alerts: func(ctx context.Context, now time.Time) adminoverview.Section {
				summary, summaryErr := alertRepo.Overview(ctx, now, cfg.AlertEvaluationEnabled)
				if summaryErr != nil {
					return adminoverview.Section{Status: "unavailable", ReasonCode: "upstream_unavailable", Items: []any{}}
				}
				return adminoverview.Section{Status: "healthy", ReasonCode: "ok", ObservedAt: &now, Items: summary}
			},
		}

		routes.Overview = overview
		routes.Logs = logHandler
		routes.Metrics = metricHandler
		routes.Alerts = alert.NewHandler(alertRepo, cfg.Auth.JWTSecret)
		routes.Events = eventHandler
		routes.Authorization = middleware.RequireSuperAdmin(users)
		routes.Management = backendhttp.NewManagementHandler(users, cfg.Auth.JWTSecret)
		routes.ExporterPlugins = exporterPluginHandler
		assembly.runAlertCheck = func(ctx context.Context) {
			if cfg.AlertEvaluationEnabled {
				alert.NewScheduler(alertRepo, metricClient).WithCounts(logRepository, eventRepository).WithLogger(logger).Run(ctx)
			}
		}
	}

	probes, err := componentmetrics.NewProbes(root, time.Second, 250*time.Millisecond, func(ctx context.Context) error {
		if err := mysqlClient.Check(ctx); err != nil {
			return err
		}
		if err := platform.CheckRuntimeSchema(ctx, mysqlClient.DB()); err != nil {
			return err
		}
		return users.ValidateBootstrap(ctx)
	})
	if err != nil {
		return nil, errors.New("initialize backend probes")
	}
	assembly.probes = probes
	assembly.router = backendhttp.NewRouter(
		backendhttp.Dependencies{
			Probes:             probes,
			MySQL:              mysqlClient,
			Redis:              redisClient,
			RabbitMQ:           rabbitMQChecker,
			Elasticsearch:      elasticsearchClient,
			Logger:             logger,
			HTTPMaxConcurrency: cfg.HTTPMaxConcurrency,
		},
		routes,
	)
	return assembly, nil
}

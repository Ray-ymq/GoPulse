package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	stdhttp "net/http"
	"os"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/bookmark"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/logging"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/outbox"
	"github.com/Ray-ymq/GoPulse/backend/internal/platform"
	rediscache "github.com/Ray-ymq/GoPulse/backend/internal/platform/redis"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
	"github.com/Ray-ymq/GoPulse/backend/internal/resourcebudget"
	searchpkg "github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type roleProfile struct {
	role       config.ServiceRole
	business   bool
	dispatcher bool
}

func profileForRole(role config.ServiceRole) (roleProfile, error) {
	if !role.IsValid() {
		return roleProfile{}, errors.New("invalid backend service role")
	}
	return roleProfile{
		role:       role,
		business:   role.RunsBusiness(),
		dispatcher: role.RunsBusiness(),
	}, nil
}

type roleResource struct {
	name  string
	close func() error
}

type roleAssembly struct {
	profile    roleProfile
	database   *sql.DB
	probes     *platform.Health
	router     stdhttp.Handler
	dispatcher *outbox.Dispatcher
	resources  []roleResource
	closed     bool
	pools      map[string]*sql.DB
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
	logger.Info("backend capacity configuration",
		slog.Int("api_mysql_max_open_conns", mysqlClient.DB().Stats().MaxOpenConnections),
		slog.Int("probe_mysql_max_open_conns", 1),
		slog.Int("outbox_mysql_max_open_conns", cfg.Outbox.MaxOpenConns),
		slog.Int("outbox_publish_concurrency", cfg.Outbox.PublishConcurrency),
		slog.Int("outbox_claim_batch", cfg.Outbox.ClaimBatch),
		slog.Duration("outbox_database_timeout", cfg.Outbox.DatabaseTimeout),
		slog.Duration("outbox_lease_duration", cfg.Outbox.LeaseDuration),
		slog.Duration("outbox_publish_timeout", cfg.Outbox.PublishTimeout),
		slog.Duration("post_detail_ttl", cfg.Redis.PostDetailTTL),
		slog.Int("rebuild_executors", cfg.Protection.ResourceRebuild))
	assembly.database = mysqlClient.DB()
	assembly.pools = map[string]*sql.DB{"api": mysqlClient.DB()}
	assembly.addResource("mysql", mysqlClient.Close)

	probeConfig := cfg.MySQL
	probeConfig.MaxOpenConns = 1
	probeConfig.MaxIdleConns = 1
	probeClient, err := platform.NewMySQL(probeConfig)
	if err != nil {
		return nil, errors.New("initialize probe MySQL client")
	}
	assembly.addResource("mysql_probe", probeClient.Close)
	assembly.pools["probe"] = probeClient.DB()
	probeUsers := user.NewMySQLRepository(probeClient.DB())
	var probeState *businessstate.Repository
	if profile.business {
		probeState, err = businessstate.NewRepository(probeClient.DB())
		if err != nil {
			return nil, errors.New("initialize probe business state")
		}
	}

	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		return nil, errors.New("initialize token manager")
	}
	cookies := auth.NewCookieManager(cfg.Auth.CookieName, cfg.Auth.CookieSecure, cfg.Auth.JWTTTL, time.Now)
	users := user.NewMySQLRepository(mysqlClient.DB())
	var state *businessstate.Repository
	if profile.business {
		state, err = businessstate.NewRepository(mysqlClient.DB(), businessstate.WithOpenRequirementsLimit(cfg.BusinessAsyncMaxOpenRequirements), businessstate.WithRetentionLimits(cfg.BusinessAsyncMaxRetainedGroups, cfg.BusinessAsyncMaxRetainedBytes))
		if err != nil {
			return nil, errors.New("initialize business state repository")
		}
		if _, err := state.Ensure(context.Background()); err != nil {
			return nil, fmt.Errorf("initialize business environment: %w", err)
		}
	}
	if err := users.ValidateBootstrap(context.Background()); err != nil {
		return nil, fmt.Errorf("validate management bootstrap: %w", err)
	}
	authentication := middleware.RequireAuthentication(cookies.Name(), tokens)

	var redisClient *platform.Redis
	var elasticsearchClient *platform.Elasticsearch
	var rabbitMQChecker *platform.RabbitMQ
	var eventOutbox *outbox.Repository
	var dispatcher *outbox.Dispatcher
	var protectionBudget *resourcebudget.Budget
	var rateLimitHandler gin.HandlerFunc
	routes := backendhttp.APIRoutes{Authentication: authentication}
	if profile.business {
		if err := state.ActivateV2(context.Background(), "phase-24-02"); err != nil {
			return nil, errors.New("activate business event v2")
		}
		if !cfg.RedisDisabled {
			redisClient = platform.NewRedis(cfg.Redis)
			assembly.addResource("redis", redisClient.Close)
		}
		protectionBudget, err = resourcebudget.New(resourcebudget.Config{
			Auth: cfg.Protection.ResourceAuth, Read: cfg.Protection.ResourceRead, Write: cfg.Protection.ResourceWrite,
			Database: cfg.Protection.ResourceDatabase, Rebuild: cfg.Protection.ResourceRebuild, Waiter: cfg.Protection.ResourceWaiter,
		})
		if err != nil {
			return nil, errors.New("initialize backend resource budget")
		}
		if cfg.Protection.RateLimitEnabled {
			trustedProxies := make([]*net.IPNet, 0, len(cfg.Protection.TrustedProxyCIDRs))
			for _, raw := range cfg.Protection.TrustedProxyCIDRs {
				_, network, parseErr := net.ParseCIDR(raw)
				if parseErr != nil {
					return nil, errors.New("parse trusted proxy network")
				}
				trustedProxies = append(trustedProxies, network)
			}
			localLimiter, localErr := ratelimit.NewLocalLimiter(cfg.Protection.LocalMaxIdentities)
			if localErr != nil {
				return nil, errors.New("initialize local rate limiter")
			}
			var sharedLimiter ratelimit.Limiter
			if cfg.RedisDisabled {
				sharedLimiter, err = ratelimit.NewMemoryLimiter(cfg.Protection.LocalMaxIdentities)
				if err != nil {
					return nil, err
				}
				logger.Info("无 Redis 业务保护已启用", slog.String("quota_scope", "per_process"), slog.Int("replica_count", cfg.ReplicaCount), slog.Int("max_identities", cfg.Protection.LocalMaxIdentities))
			} else {
				sharedLimiter = ratelimit.NewSharedLimiter(redisClient, cfg.Protection.RateLimitOperationTimeout)
			}
			rateLimitHandler, err = backendhttp.NewRateLimitHandler(sharedLimiter, localLimiter, protectionPolicies(cfg.Redis.PostDetailTTL), trustedProxies)
			if err != nil {
				return nil, errors.New("initialize shared rate limiter")
			}
		}

		elasticsearchClient, err = platform.NewElasticsearch(cfg.Elasticsearch)
		if err != nil {
			return nil, errors.New("initialize Elasticsearch client")
		}

		rabbitMQChecker, err = platform.NewRabbitMQ(cfg.RabbitMQURL)
		if err != nil {
			return nil, errors.New("initialize RabbitMQ checker")
		}
		outboxConfig := cfg.MySQL
		outboxConfig.MaxOpenConns = cfg.Outbox.MaxOpenConns
		outboxConfig.MaxIdleConns = cfg.Outbox.MaxOpenConns
		backgroundClient, err := platform.NewMySQL(outboxConfig)
		if err != nil {
			return nil, errors.New("initialize outbox MySQL client")
		}
		assembly.addResource("mysql_outbox", backgroundClient.Close)
		assembly.pools["outbox"] = backgroundClient.DB()
		eventOutbox, err = outbox.NewRepository(backgroundClient.DB(), outbox.Options{DatabaseTimeout: cfg.Outbox.DatabaseTimeout, MaxClaimBatch: cfg.Outbox.ClaimBatch, RetentionAccounting: state, EnableV2: true})
		if err != nil {
			return nil, errors.New("initialize business outbox repository")
		}
		publishers := make([]outbox.Publisher, 0, cfg.Outbox.PublishConcurrency)
		for i := 0; i < cfg.Outbox.PublishConcurrency; i++ {
			publisher, publisherErr := platform.NewRabbitMQPublisher(cfg.RabbitMQURL, platform.RabbitMQPublisherOptions{RetryDelay: cfg.Outbox.RetryDelay, SearchRetryDelay: cfg.Outbox.SearchRetryDelay})
			if publisherErr != nil {
				return nil, errors.New("initialize RabbitMQ publisher")
			}
			assembly.addResource("rabbitmq_publisher", publisher.Close)
			publishers = append(publishers, publisher)
		}
		dispatcher, err = outbox.NewDispatcher(eventOutbox, publishers[0], outbox.DispatcherOptions{
			Publishers: publishers, DatabaseTimeout: cfg.Outbox.DatabaseTimeout,
			Owner:           cfg.InstanceID + "-" + strconv.Itoa(os.Getpid()),
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
		users.WithOutbox(eventOutbox)
		authService := auth.NewService(users, passwords, tokens)
		authHandler := auth.NewHandler(authService, cookies, logger)
		posts := post.NewMySQLRepository(mysqlClient.DB(), post.RepositoryOptions{Outbox: eventOutbox, BusinessState: state, Logger: logger})
		postService := post.NewService(posts).WithLogger(logger)
		if !cfg.RedisDisabled {
			postBodyCache := rediscache.NewPostBodyRepository(redisClient, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
			rebuild := post.NewRebuildCoordinator(root, post.RebuildOptions{
				MaxActiveKeys:        cfg.Protection.ResourceRebuild,
				MaxExecutors:         cfg.Protection.ResourceRebuild,
				MaxWaitersPerKey:     cfg.Protection.ResourceWaiter,
				MaxRegisteredWaiters: cfg.Protection.ResourceWaiter,
				WaitTimeout:          cfg.Redis.OperationTimeout * 5,
				TaskTimeout:          cfg.Redis.OperationTimeout * 10,
				Budget:               protectionBudget,
			})
			postService.WithBodyCache(postBodyCache, rebuild)
			assembly.addResource("post_body_rebuild", func() error {
				shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return rebuild.Stop(shutdownContext)
			})
		}
		postHandler := post.NewHandler(postService, logger).WithBookmarkCursorSecret(cfg.Auth.JWTSecret)
		comments := comment.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
		commentService := comment.NewService(comments, postService)
		commentHandler := comment.NewHandler(commentService, logger)
		likes := like.NewMySQLRepositoryWithOutbox(mysqlClient.DB(), eventOutbox)
		likeService := like.NewService(likes, postService)
		likeHandler := like.NewHandler(likeService, logger)
		notifications, notificationErr := notification.NewRepository(mysqlClient.DB())
		if notificationErr != nil {
			return nil, errors.New("initialize notification repository")
		}
		notificationService := notification.NewService(notifications)
		notificationHandler := notification.NewHandler(notificationService, logger)
		searchRepository := searchpkg.NewElasticsearchRepository(elasticsearchClient)
		searchService := searchpkg.NewService(searchRepository, posts, cfg.Auth.JWTSecret).WithEpochProvider(state)

		routes.Auth = authHandler
		routes.Users = backendhttp.NewUserHandler(user.NewProfileService(users, cfg.Auth.JWTSecret), postService, users)
		routes.Posts = postHandler
		routes.Comments = commentHandler
		routes.Likes = likeHandler
		routes.Bookmarks = bookmark.NewHandler(bookmark.NewService(bookmark.NewMySQLRepository(mysqlClient.DB()), postService), logger)
		routes.Notifications = notificationHandler
		routes.Search = searchpkg.NewHandler(searchService)
		assembly.dispatcher = dispatcher
	}

	probes := platform.NewHealth(root, func(ctx context.Context) error {
		if err := probeClient.Check(ctx); err != nil {
			return err
		}
		if err := platform.CheckRuntimeSchema(ctx, probeClient.DB()); err != nil {
			return err
		}
		if probeState != nil {
			if _, err := probeState.Current(ctx); err != nil {
				return err
			}
		}
		return probeUsers.ValidateBootstrap(ctx)
	})
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
		func() backendhttp.APIRoutes {
			if rateLimitHandler != nil {
				routes.PublicRateLimit = rateLimitHandler
				routes.ProtectedRateLimit = rateLimitHandler
			}
			if protectionBudget != nil {
				budgetHandler := middleware.ResourceBudget(protectionBudget)
				routes.PublicResourceBudget = budgetHandler
				routes.ResourceBudget = budgetHandler
			}
			return routes
		}(),
	)
	return assembly, nil
}

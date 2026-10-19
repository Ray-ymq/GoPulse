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

	"github.com/Ray-ymq/GoPulse/backend/internal/adminoverview"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert/count"
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/bookmark"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/componentstatus"
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
	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
	"github.com/Ray-ymq/GoPulse/backend/internal/resourcebudget"
	searchpkg "github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/selfmonitor"
	"github.com/Ray-ymq/GoPulse/backend/internal/telemetrydelivery"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/gin-gonic/gin"
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
	database      *sql.DB
	probes        *componentmetrics.Probes
	router        stdhttp.Handler
	dispatcher    *outbox.Dispatcher
	sampleOutbox  func(context.Context, *componentmetrics.Backend)
	runAlertCheck func(context.Context)
	resources     []roleResource
	closed        bool
}

// initializeMetrics makes the shared Backend exposition scrapeable for a
// platform-only process. The platform role has no business outbox sampler;
// its neutral outbox fields are not a business backlog observation.
func (assembly *roleAssembly) initializeMetrics(metrics *componentmetrics.Backend) {
	if assembly == nil || metrics == nil || assembly.profile.business {
		return
	}
	metrics.ObserveOutbox(0, 0, nil)
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
	assembly.database = mysqlClient.DB()
	assembly.addResource("mysql", mysqlClient.Close)

	tokens, err := auth.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL, time.Now)
	if err != nil {
		return nil, errors.New("initialize token manager")
	}
	cookies := auth.NewCookieManager(cfg.Auth.CookieName, cfg.Auth.CookieSecure, cfg.Auth.JWTTTL, time.Now)
	users := user.NewMySQLRepository(mysqlClient.DB())
	var state *businessstate.Repository
	if profile.business {
		state, err = businessstate.NewRepository(mysqlClient.DB(), businessstate.WithOpenRequirementsLimit(cfg.BusinessAsyncMaxOpenRequirements))
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
	var stageService *businessstate.QueryService
	var protectionBudget *resourcebudget.Budget
	var rateLimitHandler gin.HandlerFunc
	routes := backendhttp.APIRoutes{Authentication: authentication}
	if profile.business {
		if err := state.ActivateV2(context.Background(), "phase-24-02"); err != nil {
			return nil, errors.New("activate business event v2")
		}
		redisClient = platform.NewRedis(cfg.Redis)
		assembly.addResource("redis", redisClient.Close)
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
			sharedLimiter := ratelimit.NewSharedLimiter(redisClient, cfg.Protection.RateLimitOperationTimeout)
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
		eventOutbox, err = outbox.NewRepository(mysqlClient.DB(), outbox.Options{MaxClaimBatch: cfg.Outbox.ClaimBatch, RetentionAccounting: state, EnableV2: true})
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
		users.WithOutbox(eventOutbox)
		authService := auth.NewService(users, passwords, tokens)
		authHandler := auth.NewHandler(authService, cookies, logger)
		postBodyCache := rediscache.NewPostBodyRepository(redisClient, cfg.Redis.PostDetailTTL, cfg.Redis.OperationTimeout)
		posts := post.NewMySQLRepository(mysqlClient.DB(), post.RepositoryOptions{Outbox: eventOutbox, BusinessState: state, Logger: logger})
		postService := post.NewService(posts).WithLogger(logger)
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
		facts, factErr := businessstate.NewPostFactReader(mysqlClient.DB())
		if factErr != nil {
			return nil, errors.New("initialize business stage fact reader")
		}
		stageService, err = businessstate.NewQueryService(state, facts, businessStageVisibility{repository: searchRepository}, nil, cfg.Trace.Enabled)
		if err != nil {
			return nil, errors.New("initialize business stage query service")
		}

		routes.Auth = authHandler
		routes.Users = backendhttp.NewUserHandler(user.NewProfileService(users, cfg.Auth.JWTSecret), postService, users)
		routes.Posts = postHandler
		routes.Comments = commentHandler
		routes.Likes = likeHandler
		routes.Bookmarks = bookmark.NewHandler(bookmark.NewService(bookmark.NewMySQLRepository(mysqlClient.DB()), postService), logger)
		routes.Notifications = notificationHandler
		routes.Search = searchpkg.NewHandler(searchService)
		routes.InternalBusinessStages = backendhttp.NewBusinessStageHandler(stageService, false)
		routes.InternalAuthorization = middleware.RequireSuperAdmin(users)
		routes.ServiceCredential = cfg.BusinessStage.Credential
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
		if stageService != nil {
			stageService.WithLogReader(businessStageLogs{service: logService})
		}
		eventRepository := eventquery.NewElasticsearchRepository(observabilityElasticsearchClient)
		eventService := eventquery.NewService(eventRepository, cfg.Auth.JWTSecret)
		eventHandler := eventquery.NewHandler(eventService)
		monitorClient, monitorErr := exporterplugin.NewClient(cfg.Monitor.URL, cfg.Monitor.APIToken, cfg.Monitor.RequestTimeout)
		if monitorErr != nil {
			return nil, errors.New("initialize monitor client")
		}
		exporterPluginHandler := exporterplugin.NewHandler(monitorClient).WithAudit(users)
		statusTargets, statusTargetErr := componentstatus.TargetsFromEnvironment()
		if statusTargetErr != nil {
			return nil, errors.New("initialize component status targets")
		}
		componentStatus, statusErr := componentstatus.New(componentstatus.Config{Targets: statusTargets})
		if statusErr != nil {
			return nil, errors.New("initialize component status")
		}
		if statusErr = componentStatus.Start(root); statusErr != nil {
			return nil, errors.New("start component status")
		}
		assembly.addResource("component_status", func() error {
			closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return componentStatus.Close(closeContext)
		})
		deliverySources, deliveryErr := telemetrydelivery.EnvironmentSources(cfg.Monitor.URL, cfg.Monitor.APIToken, os.Getenv("MARSHALLER_API_TOKEN"), 500*time.Millisecond)
		if deliveryErr != nil {
			return nil, errors.New("initialize telemetry delivery sources")
		}
		deliveryService := telemetrydelivery.New(deliverySources)
		overview := &adminoverview.Service{
			Components: adminoverview.ComponentStatus(componentStatus), KeyMetrics: adminoverview.KeyMetrics(metricClient), Plugins: adminoverview.Plugins(monitorClient, metricClient),
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
		routes.ComponentStatus = componentStatus
		routes.SelfMonitor = selfmonitor.NewClient()
		routes.TelemetryDelivery = deliveryService
		routes.Logs = logHandler
		routes.Metrics = metricHandler
		routes.Alerts = alert.NewHandler(alertRepo, cfg.Auth.JWTSecret)
		routes.Events = eventHandler
		routes.Authorization = middleware.RequireSuperAdmin(users)
		if stageService != nil {
			routes.BusinessStages = backendhttp.NewBusinessStageHandler(stageService, true)
		} else {
			stageClient, clientErr := businessstate.NewHTTPClient(cfg.BusinessStage.Endpoints, cfg.BusinessStage.Credential, nil)
			if clientErr != nil {
				return nil, errors.New("initialize business stage private client")
			}
			routes.BusinessStages = backendhttp.NewBusinessStageHandler(platformBusinessStageQuerier{
				client:          stageClient,
				logs:            logService,
				traceConfigured: cfg.Trace.Enabled,
			}, true)
		}
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
		if state != nil {
			if _, err := state.Current(ctx); err != nil {
				return err
			}
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

type businessStageVisibility struct {
	repository *searchpkg.ElasticsearchRepository
}

func (adapter businessStageVisibility) CheckVisibility(ctx context.Context, epoch string, postID, revision uint64) (businessstate.VisibilityResult, error) {
	result, err := adapter.repository.CheckVisibility(ctx, epoch, postID, revision)
	return businessstate.VisibilityResult{Matched: result.Matched, Epoch: result.Epoch, Generation: result.Generation, Version: result.Version, ReadAt: result.ReadAt}, err
}

type businessStageLogs struct {
	service *logquery.Service
}

func (adapter businessStageLogs) QueryCorrelation(ctx context.Context, query businessstate.LogCorrelationQuery) (businessstate.LogCorrelationResult, error) {
	result, err := adapter.service.QueryCorrelation(ctx, logquery.CorrelationQuery{DataEpoch: query.DataEpoch, CommandID: query.CommandID, EventID: query.EventID, PostID: query.PostID, From: query.From, To: query.To, Limit: query.Limit})
	if err != nil {
		return businessstate.LogCorrelationResult{}, err
	}
	entries := make([]businessstate.LogCorrelationEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		postID := uint64(0)
		if entry.PostID != nil {
			postID = *entry.PostID
		}
		entries = append(entries, businessstate.LogCorrelationEntry{
			Timestamp: entry.Timestamp, Level: entry.Level, Service: entry.Service, InstanceID: entry.InstanceID,
			Module: entry.Module, Message: entry.Message, TraceID: entry.TraceID, SpanID: entry.SpanID,
			AttemptID: entry.AttemptID, EventID: entry.EventID, CommandID: entry.CommandID,
			DataEpoch: entry.DataEpoch, PostID: postID,
		})
	}
	return businessstate.LogCorrelationResult{Entries: entries, Truncated: result.Truncated, Quality: result.Quality}, nil
}

// platformBusinessStageQuerier keeps durable state reads on the business
// role, while completing the public stage response with the platform-owned
// observability evidence. A separated platform must not expose the business
// role's logs_unavailable placeholder as its own result.
type platformBusinessStageQuerier struct {
	client          *businessstate.HTTPClient
	logs            *logquery.Service
	traceConfigured bool
}

func (querier platformBusinessStageQuerier) Query(ctx context.Context, query businessstate.StageQuery) (businessstate.StageResponse, error) {
	return querier.query(ctx, query, "")
}

func (querier platformBusinessStageQuerier) QueryWithSession(ctx context.Context, query businessstate.StageQuery, cookie string) (businessstate.StageResponse, error) {
	return querier.query(ctx, query, cookie)
}

func (querier platformBusinessStageQuerier) query(ctx context.Context, query businessstate.StageQuery, cookie string) (businessstate.StageResponse, error) {
	if querier.client == nil {
		return businessstate.StageResponse{}, businessstate.ErrStageRemote
	}
	response, err := querier.client.QueryWithSession(ctx, query, cookie)
	if err != nil || len(query.LogSources) == 0 {
		return response, err
	}

	now := time.Now().UTC()
	from, to := query.LogFrom, query.LogTo
	if from.IsZero() {
		to = now
		from = to.Add(-15 * time.Minute)
	}
	correlationQuery := logquery.CorrelationQuery{DataEpoch: query.DataEpoch, From: from, To: to, Limit: businessstate.StageMaximumLogs}
	switch {
	case query.CommandID != "":
		correlationQuery.CommandID = query.CommandID
	case query.EventID != "":
		correlationQuery.EventID = query.EventID
	default:
		correlationQuery.PostID = query.PostID
	}

	if querier.logs == nil {
		return querier.setStageEvidenceUnavailable(response, query, now), nil
	}
	correlation, err := querier.logs.QueryCorrelation(ctx, correlationQuery)
	if err != nil {
		return querier.setStageEvidenceUnavailable(response, query, now), nil
	}
	if containsStageSource(query.LogSources, businessstate.StageSourceLogs) {
		logs := businessstate.StageSource{Source: businessstate.StageSourceLogs, ReadAt: stageTimePtr(now), Quality: correlation.Quality, Truncated: correlation.Truncated}
		for _, entry := range correlation.Entries {
			logs.Entries = append(logs.Entries, platformStageLogEntry(entry))
		}
		if logs.Quality == "" {
			logs.Quality = businessstate.StageQualityNoEvidence
		}
		response.Evidence.Logs = logs
	}
	if containsStageSource(query.LogSources, businessstate.StageSourceTrace) {
		trace := businessstate.StageSource{Source: businessstate.StageSourceTrace, ReadAt: stageTimePtr(now)}
		for _, entry := range correlation.Entries {
			if entry.TraceID != "" && !containsStageValue(trace.TraceIDs, entry.TraceID) {
				trace.TraceIDs = append(trace.TraceIDs, entry.TraceID)
			}
		}
		if len(trace.TraceIDs) > 0 {
			trace.Quality = businessstate.StageQualityKnown
		} else if !querier.traceConfigured {
			trace.Quality = businessstate.StageQualityNotConfigured
			trace.Sampling = "trace_query_not_configured"
		} else {
			trace.Quality = businessstate.StageQualitySampling
			trace.Sampling = "trace_id_not_observed"
		}
		response.Evidence.Trace = trace
	}
	return response, nil
}

func (querier platformBusinessStageQuerier) setStageEvidenceUnavailable(response businessstate.StageResponse, query businessstate.StageQuery, now time.Time) businessstate.StageResponse {
	if containsStageSource(query.LogSources, businessstate.StageSourceLogs) {
		response.Evidence.Logs = businessstate.StageSource{Source: businessstate.StageSourceLogs, ReadAt: stageTimePtr(now), Quality: businessstate.StageQualityBackendError, Reason: "logs_unavailable"}
	}
	if containsStageSource(query.LogSources, businessstate.StageSourceTrace) {
		response.Evidence.Trace = businessstate.StageSource{Source: businessstate.StageSourceTrace, ReadAt: stageTimePtr(now), Quality: businessstate.StageQualityBackendError, Reason: "trace_evidence_unavailable"}
	}
	return response
}

func platformStageLogEntry(entry logquery.Entry) businessstate.StageLogEntry {
	postID := uint64(0)
	if entry.PostID != nil {
		postID = *entry.PostID
	}
	return businessstate.StageLogEntry{
		Timestamp: entry.Timestamp, Level: entry.Level, Service: entry.Service, InstanceID: entry.InstanceID,
		Module: entry.Module, Message: entry.Message, TraceID: entry.TraceID, SpanID: entry.SpanID,
		AttemptID: entry.AttemptID, EventID: entry.EventID, CommandID: entry.CommandID,
		DataEpoch: entry.DataEpoch, PostID: postID,
	}
}

func stageTimePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func containsStageSource(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsStageValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

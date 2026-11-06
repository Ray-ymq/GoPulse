package main

import (
	"errors"
	"log/slog"
	"net"

	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/ratelimit"
	"github.com/Ray-ymq/GoPulse/backend/internal/resourcebudget"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
)

// 独立可观测系统通过 MySQL 完成鉴权，并在进程内限制公开鉴权请求。
func configureStandaloneAuth(routes *backendhttp.APIRoutes, cfg config.Config, users user.Repository, tokens *auth.TokenManager, cookies *auth.CookieManager, logger *slog.Logger) error {
	passwords, err := auth.NewPasswordManager()
	if err != nil {
		return errors.New("初始化独立可观测系统密码管理器失败")
	}
	routes.Auth = auth.NewHandler(auth.NewService(users, passwords, tokens), cookies, logger)
	budget, err := resourcebudget.New(resourcebudget.Config{
		Auth: cfg.Protection.ResourceAuth, Read: cfg.Protection.ResourceRead, Write: cfg.Protection.ResourceWrite,
		Database: cfg.Protection.ResourceDatabase, Rebuild: cfg.Protection.ResourceRebuild, Waiter: cfg.Protection.ResourceWaiter,
	})
	if err != nil {
		return err
	}
	routes.PublicResourceBudget = middleware.ResourceBudget(budget)
	if !cfg.Protection.RateLimitEnabled {
		return nil
	}
	local, err := ratelimit.NewLocalLimiter(cfg.Protection.LocalMaxIdentities)
	if err != nil {
		return err
	}
	shared, err := ratelimit.NewMemoryLimiter(cfg.Protection.LocalMaxIdentities)
	if err != nil {
		return err
	}
	var trusted []*net.IPNet
	for _, raw := range cfg.Protection.TrustedProxyCIDRs {
		_, network, parseErr := net.ParseCIDR(raw)
		if parseErr != nil {
			return parseErr
		}
		trusted = append(trusted, network)
	}
	routes.PublicRateLimit, err = backendhttp.NewRateLimitHandler(shared, local, protectionPolicies(0), trusted)
	return err
}

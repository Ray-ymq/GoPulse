package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	backendhttp "github.com/Ray-ymq/GoPulse/backend/internal/http"
)

func TestStandaloneAuthenticationDoesNotRegisterBusinessRoutes(t *testing.T) {
	cfg := config.Config{Protection: config.ProtectionConfig{
		RateLimitEnabled: true, LocalMaxIdentities: 1024,
		ResourceAuth: 16, ResourceRead: 64, ResourceWrite: 32,
		ResourceDatabase: 32, ResourceRebuild: 8, ResourceWaiter: 256,
	}}
	routes := backendhttp.APIRoutes{}
	if err := configureStandaloneAuth(&routes, cfg, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if routes.PublicRateLimit == nil || routes.PublicResourceBudget == nil {
		t.Fatal("authentication protection is missing")
	}
	router := backendhttp.NewRouter(backendhttp.Dependencies{}, routes)
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/posts") || strings.HasPrefix(route.Path, "/api/v1/search") {
			t.Fatalf("business route registered: %s", route.Path)
		}
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"!","password":"short"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "127.0.0.1:1234"
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("login status=%d, body=%s", response.Code, response.Body.String())
	}
}

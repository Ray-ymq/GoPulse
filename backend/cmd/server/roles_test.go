package main

import (
	"context"
	stdhttp "net/http"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

func TestProfileForRoleOwnsOnlyItsBackgroundWork(t *testing.T) {
	tests := []struct {
		role               config.ServiceRole
		business, platform bool
		dispatcher         bool
	}{
		{role: config.ServiceRoleBusiness, business: true, dispatcher: true},
		{role: config.ServiceRolePlatform, platform: true},
		{role: config.ServiceRoleCombined, business: true, platform: true, dispatcher: true},
	}
	for _, test := range tests {
		t.Run(string(test.role), func(t *testing.T) {
			profile, err := profileForRole(test.role)
			if err != nil {
				t.Fatalf("profileForRole() error = %v", err)
			}
			if profile.business != test.business || profile.platform != test.platform || profile.dispatcher != test.dispatcher {
				t.Fatalf("profile = %#v, want business=%v platform=%v dispatcher=%v", profile, test.business, test.platform, test.dispatcher)
			}
		})
	}
}

func TestProfileForRoleRejectsUnknownRoleBeforeAssembly(t *testing.T) {
	if _, err := profileForRole(config.ServiceRole("worker")); err == nil {
		t.Fatal("profileForRole(worker) error = nil")
	}
}

func TestServeWithDispatcherAllowsPlatformWithoutDispatcher(t *testing.T) {
	server := newHTTPServer("127.0.0.1:0", stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, _ *stdhttp.Request) {
		writer.WriteHeader(stdhttp.StatusNoContent)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- serveWithDispatcher(ctx, server, nil, nil) }()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serveWithDispatcher() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("platform server did not stop without a dispatcher")
	}
}

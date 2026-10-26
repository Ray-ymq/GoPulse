package httpserver

import (
	"context"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeManager struct{ calls int }

func (f *fakeManager) List() []plugin.Status             { f.calls++; return nil }
func (f *fakeManager) Get(string) (plugin.Status, error) { f.calls++; return plugin.Status{}, nil }
func (f *fakeManager) Install(context.Context, string) (plugin.Status, error) {
	f.calls++
	return plugin.Status{}, nil
}
func (f *fakeManager) Start(context.Context, string) (plugin.Status, error) {
	f.calls++
	return plugin.Status{}, nil
}
func (f *fakeManager) Stop(context.Context, string) (plugin.Status, error) {
	f.calls++
	return plugin.Status{}, nil
}
func (f *fakeManager) Update(context.Context, string, string) (plugin.Status, error) {
	f.calls++
	return plugin.Status{}, nil
}
func TestInternalRoutesRequireBearerTokenBeforeManager(t *testing.T) {
	manager := &fakeManager{}
	server := New("01234567890123456789012345678901", t.TempDir(), manager, nil)
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/exporter-plugins", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || manager.calls != 0 {
		t.Fatalf("status=%d calls=%d", response.Code, manager.calls)
	}
}

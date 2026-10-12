package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/gin-gonic/gin"
)

const testBusinessEpoch = "123e4567-e89b-12d3-a456-426614174000"
const testBusinessCommand = "123e4567-e89b-12d3-a456-426614174001"

type recordingBusinessStageQuerier struct {
	queries []businessstate.StageQuery
}

func (querier *recordingBusinessStageQuerier) Query(_ context.Context, query businessstate.StageQuery) (businessstate.StageResponse, error) {
	querier.queries = append(querier.queries, query)
	return businessstate.StageResponse{RequestedEpoch: query.DataEpoch}, nil
}

func newBusinessStageTestRouter(querier businessStageQuerier, public bool) *gin.Engine {
	router := gin.New()
	router.GET("/business-stages", NewBusinessStageHandler(querier, public).WithClock(func() time.Time {
		return time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	}).Handle)
	return router
}

func TestBusinessStageHandlerRejectsAmbiguousAndUnknownSelectors(t *testing.T) {
	querier := &recordingBusinessStageQuerier{}
	router := newBusinessStageTestRouter(querier, false)
	for _, rawQuery := range []string{
		"epoch=" + testBusinessEpoch,
		"epoch=" + testBusinessEpoch + "&command_id=" + testBusinessCommand + "&event_id=" + testBusinessCommand,
		"epoch=" + testBusinessEpoch + "&command_id=" + testBusinessCommand + "&unknown=x",
		"epoch=" + testBusinessEpoch + "&command_id=" + testBusinessCommand + "&check_visibility=1",
	} {
		request := httptest.NewRequest(http.MethodGet, "/business-stages?"+rawQuery, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400; body=%s", rawQuery, response.Code, response.Body.String())
		}
	}
	if len(querier.queries) != 0 {
		t.Fatalf("invalid requests reached query service: %#v", querier.queries)
	}
}

func TestBusinessStageHandlerParsesPublicEvidenceWindowAndCookie(t *testing.T) {
	querier := &recordingSessionBusinessStageQuerier{}
	router := newBusinessStageTestRouter(querier, true)
	request := httptest.NewRequest(http.MethodGet, "/business-stages?epoch="+testBusinessEpoch+"&post_id=17&include=logs,trace&from=2026-10-12T09:00:00Z&to=2026-10-12T09:30:00Z", nil)
	request.Header.Set("Cookie", "gopulse_session=session-value")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if len(querier.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(querier.queries))
	}
	query := querier.queries[0]
	if query.DataEpoch != testBusinessEpoch || query.PostID != 17 || len(query.LogSources) != 2 || query.LogFrom.IsZero() || query.LogTo.IsZero() {
		t.Fatalf("parsed query = %#v", query)
	}
	if querier.cookie != "gopulse_session=session-value" {
		t.Fatalf("forwarded cookie = %q", querier.cookie)
	}
}

func TestBusinessStageHandlerParsesInternalEvidenceWindow(t *testing.T) {
	querier := &recordingBusinessStageQuerier{}
	router := newBusinessStageTestRouter(querier, false)
	request := httptest.NewRequest(http.MethodGet, "/business-stages?epoch="+testBusinessEpoch+"&post_id=17&include=logs,trace&from=2026-10-12T09:00:00Z&to=2026-10-12T09:30:00Z", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if len(querier.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(querier.queries))
	}
	query := querier.queries[0]
	if query.DataEpoch != testBusinessEpoch || query.PostID != 17 || len(query.LogSources) != 2 || query.LogFrom.IsZero() || query.LogTo.IsZero() {
		t.Fatalf("parsed query = %#v", query)
	}
}

func TestBusinessStageHandlerRequiresEvidenceWindowToNameSources(t *testing.T) {
	querier := &recordingBusinessStageQuerier{}
	router := newBusinessStageTestRouter(querier, true)
	request := httptest.NewRequest(http.MethodGet, "/business-stages?epoch="+testBusinessEpoch+"&command_id="+testBusinessCommand+"&from=2026-10-12T09:00:00Z&to=2026-10-12T09:30:00Z", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "validation_failed") {
		t.Fatalf("status=%d body=%s, want validation failure", response.Code, response.Body.String())
	}
}

type recordingSessionBusinessStageQuerier struct {
	recordingBusinessStageQuerier
	cookie string
}

func (querier *recordingSessionBusinessStageQuerier) QueryWithSession(ctx context.Context, query businessstate.StageQuery, cookie string) (businessstate.StageResponse, error) {
	querier.cookie = cookie
	return querier.Query(ctx, query)
}

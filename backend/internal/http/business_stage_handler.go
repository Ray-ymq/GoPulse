package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/businessstate"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

type businessStageQuerier interface {
	Query(context.Context, businessstate.StageQuery) (businessstate.StageResponse, error)
}

type businessStageSessionQuerier interface {
	QueryWithSession(context.Context, businessstate.StageQuery, string) (businessstate.StageResponse, error)
}

type BusinessStageHandler struct {
	querier businessStageQuerier
	public  bool
	now     func() time.Time
}

func NewBusinessStageHandler(querier businessStageQuerier, public bool) *BusinessStageHandler {
	return &BusinessStageHandler{querier: querier, public: public, now: time.Now}
}

func (handler *BusinessStageHandler) WithClock(clock func() time.Time) *BusinessStageHandler {
	if handler != nil && clock != nil {
		handler.now = clock
	}
	return handler
}

func (handler *BusinessStageHandler) Handle(c *gin.Context) {
	if handler == nil || handler.querier == nil {
		response.Error(c, apperror.New(apperror.CodeBusinessStageUnavailable, "business stage query is unavailable"))
		return
	}
	query, err := parseBusinessStageQuery(c, handler.public, handler.now().UTC())
	if err != nil {
		response.Error(c, err)
		return
	}
	var result businessstate.StageResponse
	if handler.public {
		if sessionQuerier, ok := handler.querier.(businessStageSessionQuerier); ok {
			result, err = sessionQuerier.QueryWithSession(c.Request.Context(), query, c.GetHeader("Cookie"))
		} else {
			result, err = handler.querier.Query(c.Request.Context(), query)
		}
	} else {
		result, err = handler.querier.Query(c.Request.Context(), query)
	}
	if err != nil {
		handleBusinessStageError(c, err)
		return
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 256<<10 {
		response.Error(c, apperror.New(apperror.CodeBusinessStageUnavailable, "business stage response is unavailable"))
		return
	}
	response.Data(c, http.StatusOK, result)
}

func parseBusinessStageQuery(c *gin.Context, public bool, now time.Time) (businessstate.StageQuery, error) {
	values := c.Request.URL.Query()
	allowed := map[string]bool{"epoch": true, "command_id": true, "event_id": true, "post_id": true, "check_visibility": true}
	if public {
		allowed["from"], allowed["to"], allowed["include"] = true, true, true
	}
	for key, list := range values {
		if !allowed[key] || len(list) != 1 || list[0] == "" || !utf8.ValidString(list[0]) {
			return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage query parameters are invalid")
		}
	}
	query := businessstate.StageQuery{DataEpoch: singleQueryValue(values, "epoch")}
	query.CommandID = singleQueryValue(values, "command_id")
	query.EventID = singleQueryValue(values, "event_id")
	if raw := singleQueryValue(values, "post_id"); raw != "" {
		for _, character := range raw {
			if character < '0' || character > '9' {
				return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage query parameters are invalid")
			}
		}
		postID, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil || postID == 0 || (len(raw) > 1 && raw[0] == '0') {
			return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage query parameters are invalid")
		}
		query.PostID = postID
	}
	if raw := singleQueryValue(values, "check_visibility"); raw != "" {
		if raw != "true" && raw != "false" {
			return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage query parameters are invalid")
		}
		query.CheckVisibility = raw == "true"
	}
	if public {
		include := singleQueryValue(values, "include")
		if include != "" {
			for _, source := range strings.Split(include, ",") {
				if source != businessstate.StageSourceLogs && source != businessstate.StageSourceTrace || containsString(query.LogSources, source) {
					return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage evidence sources are invalid")
				}
				query.LogSources = append(query.LogSources, source)
			}
		}
		fromRaw, toRaw := singleQueryValue(values, "from"), singleQueryValue(values, "to")
		if fromRaw != "" || toRaw != "" {
			if fromRaw == "" || toRaw == "" || len(query.LogSources) == 0 {
				return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage evidence window is invalid")
			}
			var parseErr error
			query.LogFrom, parseErr = time.Parse(time.RFC3339Nano, fromRaw)
			if parseErr != nil || query.LogFrom.Location() != time.UTC {
				return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage evidence window is invalid")
			}
			query.LogTo, parseErr = time.Parse(time.RFC3339Nano, toRaw)
			if parseErr != nil || query.LogTo.Location() != time.UTC || !query.LogFrom.Before(query.LogTo) || query.LogTo.Sub(query.LogFrom) > 24*time.Hour || query.LogTo.After(now.Add(5*time.Minute)) {
				return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage evidence window is invalid")
			}
		}
	}
	if err := query.Validate(); err != nil {
		return businessstate.StageQuery{}, apperror.New(apperror.CodeValidationFailed, "business stage query parameters are invalid")
	}
	return query, nil
}

func handleBusinessStageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, businessstate.ErrStageBusy):
		response.Error(c, apperror.New(apperror.CodeBusinessStageBusy, "business stage query is temporarily busy"))
	case errors.Is(err, businessstate.ErrStageUnauthorized):
		response.Error(c, apperror.New(apperror.CodeAuthenticationRequired, "authentication is required"))
	case errors.Is(err, businessstate.ErrStageRemote), errors.Is(err, businessstate.ErrStageBackend), errors.Is(err, businessstate.ErrStageResponseSize):
		response.Error(c, apperror.New(apperror.CodeBusinessStageUnavailable, "business stage query is temporarily unavailable"))
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		response.Error(c, apperror.New(apperror.CodeBusinessStageUnavailable, "business stage query is temporarily unavailable"))
	default:
		if _, ok := apperror.As(err); ok {
			response.Error(c, err)
			return
		}
		response.Error(c, apperror.New(apperror.CodeBusinessStageUnavailable, "business stage query is temporarily unavailable"))
	}
}

func singleQueryValue(values map[string][]string, key string) string {
	if list, ok := values[key]; ok && len(list) == 1 {
		return list[0]
	}
	return ""
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

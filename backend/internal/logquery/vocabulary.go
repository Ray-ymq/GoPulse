package logquery

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/alert/count"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"sort"
)

// logVocabulary mirrors the Schema v1 source/module/message contract enforced
// by LogMonitor and Marshaller. Query filters may only select combinations that
// a valid application log can contain.
var workerMessages = map[string]struct{}{
	"event ignored": {}, "event processed": {}, "message acknowledgement failed": {},
	"retry publish failed": {}, "message requeue failed": {}, "event retry scheduled": {},
	"dead letter publish failed": {}, "event dead lettered": {}, "connection unavailable": {},
	"connection restored": {}, "session close failed": {}, "session interrupted": {},
	"delivery stop failed": {}, "shutdown timeout": {},
}

var logVocabulary = map[string]map[string]map[string]struct{}{
	"backend": {
		"http": {"request id generation failed": {}, "http request completed": {}, "http panic recovered": {}},
		"auth": {"user registered": {}, "user logged in": {}, "user logged out": {}},
		"post": {"post created": {}}, "comment": {"comment created": {}},
		"like": {"post liked": {}, "post unliked": {}}, "notification": {"notification marked read": {}},
		"cache":     {"post detail cache fill failed": {}, "post detail cache read failed": {}, "post detail cache invalidation failed": {}},
		"outbox":    {"outbox cleanup failed": {}, "outbox claim failed": {}, "outbox event invalid": {}, "outbox publish failed": {}, "outbox mark published failed": {}, "outbox event published": {}, "outbox release failed": {}},
		"lifecycle": {"backend listening": {}, "backend stopped": {}, "backend server failed": {}, "backend shutdown started": {}, "backend shutdown failed": {}, "resource close failed": {}},
	},
	"business-worker": {
		"lifecycle": {"business worker started": {}, "business worker stopped": {}, "business worker initialization failed": {}, "resource close failed": {}},
		"worker":    workerMessages, "notification": workerMessages,
	},
	"search-indexer": {
		"lifecycle": {"search indexer started": {}, "search indexer stopped": {}, "search indexer initialization failed": {}, "resource close failed": {}},
		"worker":    workerMessages, "search": workerMessages,
	},
	"search-reindex": {
		"search": {"search reindex arguments invalid": {}, "search reindex initialization failed": {}, "search reindex started": {}, "search reindex skipped": {}, "search reindex completed": {}, "search reindex failed": {}, "resource close failed": {}},
	},
}

func validLogVocabulary(service, module, message string) bool {
	if service == "" && module == "" && message == "" {
		return true
	}
	for candidateService, modules := range logVocabulary {
		if service != "" && service != candidateService {
			continue
		}
		for candidateModule, messages := range modules {
			if module != "" && module != candidateModule {
				continue
			}
			if message == "" {
				return true
			}
			if _, ok := messages[message]; ok {
				return true
			}
		}
	}
	return false
}

var logErrorCodes = []string{string(apperror.CodeValidationFailed), string(apperror.CodeAuthenticationRequired), string(apperror.CodePermissionDenied), string(apperror.CodeInvalidCredentials), string(apperror.CodeUsernameConflict), string(apperror.CodePostNotFound), string(apperror.CodeNotificationNotFound), string(apperror.CodeSearchUnavailable), string(apperror.CodeMetricsUnavailable), string(apperror.CodeLogsUnavailable), string(apperror.CodeEventsUnavailable), string(apperror.CodePluginPackageInvalid), string(apperror.CodePluginNotFound), string(apperror.CodePluginConflict), string(apperror.CodePluginOperationInProgress), string(apperror.CodePluginOperationFailed), string(apperror.CodeMonitorUnavailable), string(apperror.CodeInternal)}

func validErrorCode(value string) bool {
	for _, v := range logErrorCodes {
		if v == value {
			return true
		}
	}
	return false
}

// AlertCatalog exposes copies of the same vocabulary used by public queries.
func AlertCatalog() count.Catalog {
	c := count.Catalog{Fields: map[string][]string{"level": append([]string(nil), logLevels...), "error_code": append([]string(nil), logErrorCodes...)}, Reducers: []string{"count"}, MinimumSelectors: 1}
	sets := map[string]map[string]bool{"service": {}, "module": {}, "message": {}}
	for service, modules := range logVocabulary {
		for module, messages := range modules {
			for message := range messages {
				c.Tuples = append(c.Tuples, map[string]string{"service": service, "module": module, "message": message})
				sets["service"][service] = true
				sets["module"][module] = true
				sets["message"][message] = true
			}
		}
	}
	for k, values := range sets {
		for v := range values {
			c.Fields[k] = append(c.Fields[k], v)
		}
		sort.Strings(c.Fields[k])
	}
	sort.Slice(c.Tuples, func(i, j int) bool {
		a, b := c.Tuples[i], c.Tuples[j]
		return a["service"]+a["module"]+a["message"] < b["service"]+b["module"]+b["message"]
	})
	return c
}
func ValidateAlertSelector(labels map[string]string) bool {
	if len(labels) == 0 {
		return false
	}
	for k, v := range labels {
		if v == "" {
			return false
		}
		switch k {
		case "service", "module", "message":
		case "level":
			if !validLevel(v) {
				return false
			}
		case "error_code":
			if !validErrorCode(v) {
				return false
			}
		default:
			return false
		}
	}
	return validLogVocabulary(labels["service"], labels["module"], labels["message"])
}

var logLevels = []string{"info", "warn", "error"}

func validLevel(v string) bool {
	if v == "" {
		return true
	}
	for _, level := range logLevels {
		if v == level {
			return true
		}
	}
	return false
}

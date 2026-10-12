package http

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/adminoverview"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert"
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/bookmark"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"github.com/gin-gonic/gin"
)

// openAPISpecPath 指向随服务发布的 HTTP 契约描述。
const openAPISpecPath = "../../api/openapi.yaml"

type routeKey struct {
	path   string
	method string
}

type specOperation struct {
	line        int
	hasTags     bool
	hasResponse bool
	inResponses bool
}

// TestOpenAPIcoversRegisteredRoutes 双向比对 api/openapi.yaml 与实际注册的路由，
// 避免契约描述与代码漂移：代码新增或删除端点时该测试失败。
func TestOpenAPIcoversRegisteredRoutes(t *testing.T) {
	spec, err := readOpenAPIOperations(openAPISpecPath)
	if err != nil {
		t.Fatalf("parse %s: %v", openAPISpecPath, err)
	}
	registered := registeredRoutes(t)

	missing := make([]routeKey, 0)
	for key := range registered {
		if _, ok := spec[key]; !ok {
			missing = append(missing, key)
		}
	}
	extra := make([]routeKey, 0)
	for key := range spec {
		if !registered[key] {
			extra = append(extra, key)
		}
	}
	sortRoutes(missing)
	sortRoutes(extra)

	for _, key := range missing {
		t.Errorf("%s is registered but missing from %s", formatRoute(key), openAPISpecPath)
	}
	for _, key := range extra {
		t.Errorf("%s is described at line %d of %s but not registered",
			formatRoute(key), spec[key].line, openAPISpecPath)
	}
}

// TestOpenAPIOperationsDeclareTagsAndResponses 要求每个操作都带分组标签与至少一个成功响应，
// 这样生成的文档才有导航结构和可用的返回示例。
func TestOpenAPIOperationsDeclareTagsAndResponses(t *testing.T) {
	spec, err := readOpenAPIOperations(openAPISpecPath)
	if err != nil {
		t.Fatalf("parse %s: %v", openAPISpecPath, err)
	}
	for key, operation := range spec {
		if !operation.hasTags {
			t.Errorf("%s at line %d declares no tag", formatRoute(key), operation.line)
		}
		if !operation.hasResponse {
			t.Errorf("%s at line %d declares no success response", formatRoute(key), operation.line)
		}
	}
}

func formatRoute(key routeKey) string {
	return strings.ToUpper(key.method) + " " + key.path
}

func sortRoutes(routes []routeKey) {
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path != routes[j].path {
			return routes[i].path < routes[j].path
		}
		return routes[i].method < routes[j].method
	})
}

// registeredRoutes 汇集 business 与 platform 两种角色装配后实际存在的全部路由。
func registeredRoutes(t *testing.T) map[routeKey]bool {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	probes, err := componentmetrics.NewProbes(root, time.Second, time.Second, nil)
	if err != nil {
		t.Fatalf("componentmetrics.NewProbes() error = %v", err)
	}
	passThrough := func(c *gin.Context) { c.Next() }
	routers := []*gin.Engine{
		NewRouter(Dependencies{Probes: probes}, APIRoutes{
			Auth:           auth.NewHandler(nil, nil),
			Authentication: passThrough,
			Users:          NewUserHandler(nil, nil, &user.MySQLRepository{}),
			Posts:          post.NewHandler(nil),
			Comments:       comment.NewHandler(nil),
			Bookmarks:      bookmark.NewHandler(nil),
			Likes:          like.NewHandler(nil),
			Search:         search.NewHandler(nil),
			Notifications:  notification.NewHandler(nil),
		}),
		NewRouter(Dependencies{Probes: probes}, APIRoutes{
			Authentication:  passThrough,
			Authorization:   passThrough,
			Overview:        &adminoverview.Service{},
			Metrics:         metricquery.NewHandler(nil),
			Logs:            logquery.NewHandler(nil),
			Events:          eventquery.NewHandler(nil),
			Alerts:          alert.NewHandler(nil, "test-secret"),
			Management:      NewManagementHandler(nil, "test-secret"),
			ExporterPlugins: exporterplugin.NewHandler(nil),
		}),
	}
	registered := make(map[routeKey]bool)
	for _, router := range routers {
		for _, route := range router.Routes() {
			registered[routeKey{path: ginPathToOpenAPI(route.Path), method: strings.ToLower(route.Method)}] = true
		}
	}
	return registered
}

// ginPathToOpenAPI 把 gin 的 ":name" 参数语法转换为 OpenAPI 的 "{name}"。
func ginPathToOpenAPI(path string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[index] = "{" + strings.TrimPrefix(segment, ":") + "}"
		}
	}
	return strings.Join(segments, "/")
}

// readOpenAPIOperations 逐行读取 paths 块。
// 规格文件使用固定的两空格缩进：路径键 2 格、方法键 4 格、操作键 6 格、响应码 8 格；
// 该测试同时承担这一格式的守卫职责。
func readOpenAPIOperations(path string) (map[routeKey]specOperation, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	operations := make(map[routeKey]specOperation)
	inPaths := false
	currentPath := ""
	currentMethod := ""
	current := specOperation{}
	flush := func() {
		if currentPath == "" || currentMethod == "" {
			return
		}
		operations[routeKey{path: currentPath, method: currentMethod}] = current
	}
	for index, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if trimmed == "paths:" {
				inPaths = true
				continue
			}
			if inPaths {
				break
			}
			continue
		}
		if !inPaths {
			continue
		}
		body := yamlKey(trimmed)
		switch indent {
		case 2:
			flush()
			currentPath = body
			currentMethod = ""
			current = specOperation{}
		case 4:
			if !isHTTPMethod(body) {
				continue
			}
			flush()
			currentMethod = body
			current = specOperation{line: index + 1}
		case 6:
			if currentMethod == "" {
				continue
			}
			switch body {
			case "tags":
				current.hasTags = true
			case "responses":
				current.inResponses = true
			default:
				current.inResponses = false
			}
		case 8:
			if currentMethod == "" || !current.inResponses {
				continue
			}
			code := strings.Trim(body, "'\"")
			if len(code) == 3 && code[0] == '2' {
				current.hasResponse = true
			}
		}
	}
	flush()
	if len(operations) == 0 {
		return nil, fmt.Errorf("no operations found")
	}
	return operations, nil
}

func isHTTPMethod(value string) bool {
	switch value {
	case "get", "put", "post", "delete", "patch", "head", "options", "trace":
		return true
	default:
		return false
	}
}

// yamlKey 取出一行的键名，忽略 `key:` 与 `key: [value]` 两种写法。
func yamlKey(line string) string {
	if index := strings.Index(line, ":"); index >= 0 {
		return line[:index]
	}
	return line
}

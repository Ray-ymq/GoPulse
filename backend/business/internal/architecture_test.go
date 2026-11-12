package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const internalPrefix = "github.com/Ray-ymq/GoPulse/backend/internal/"

func boundaryViolation(source, imported string) string {
	if strings.HasPrefix(source, "modules/") && imported == "github.com/gin-gonic/gin" {
		return "业务模块不得依赖 Gin"
	}
	if !strings.HasPrefix(imported, internalPrefix) {
		return ""
	}
	target := strings.TrimPrefix(imported, internalPrefix)
	if strings.HasPrefix(target, "testkit/") && !strings.HasPrefix(source, "testkit/") {
		return "生产代码不得依赖测试支持"
	}
	switch {
	case strings.HasPrefix(source, "modules/bus/"):
		return "事件契约不得依赖其他内部包"
	case strings.HasPrefix(source, "modules/businessstate/"):
		if target != "modules/bus" {
			return "一致性状态只能依赖事件契约"
		}
	case strings.HasPrefix(source, "modules/"):
		if strings.HasPrefix(target, "transport/") || strings.HasPrefix(target, "async/worker") || strings.HasPrefix(target, "platform/postcache") {
			return "业务模块不得依赖协议入口、消费运行或具体缓存适配器"
		}
	case strings.HasPrefix(source, "async/"):
		if strings.HasPrefix(target, "transport/") || (strings.HasPrefix(target, "modules/") && target != "modules/bus" && target != "modules/businessstate") {
			return "异步执行只能通过事件和状态契约连接业务"
		}
	case strings.HasPrefix(source, "platform/"):
		if strings.HasPrefix(target, "transport/") || strings.HasPrefix(target, "async/worker") {
			return "基础设施不得依赖协议入口或消费运行"
		}
		isPostCacheContract := strings.HasPrefix(source, "platform/postcache/") && target == "modules/post"
		if strings.HasPrefix(target, "modules/") && target != "modules/bus" && !isPostCacheContract {
			return "只有帖子缓存适配器可以依赖对应业务接口"
		}
	case strings.HasPrefix(source, "shared/"), strings.HasPrefix(source, "logging/"):
		if !strings.HasPrefix(target, "shared/") && target != "logging" {
			return "公共能力不得依赖上层实现"
		}
	case strings.HasPrefix(source, "transport/http/handlers/"):
		if target == "platform" || strings.HasPrefix(target, "platform/") || strings.HasPrefix(target, "async/") {
			return "HTTP Handler 必须通过业务接口访问功能"
		}
	}
	return ""
}

func TestPackageBoundaries(t *testing.T) {
	layers := map[string]bool{"modules": true, "transport": true, "async": true, "platform": true, "shared": true, "logging": true, "testkit": true}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		source := filepath.ToSlash(path)
		if entry.IsDir() {
			if source != "." && !layers[strings.Split(source, "/")[0]] {
				t.Errorf("目录未归属已定义的职责层：%s", source)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if reason := boundaryViolation(source, imported); reason != "" {
				t.Errorf("%s：%s -> %s", reason, path, imported)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		source, target string
		rejected       bool
	}{
		{"modules/post/service.go", "transport/http/response", true},
		{"modules/post/service.go", "async/worker", true},
		{"modules/post/service.go", "platform/postcache", true},
		{"modules/post/repository_create.go", "async/outbox", false},
		{"modules/businessstate/commands.go", "modules/bus", false},
		{"modules/bus/envelope.go", "modules/post", true},
		{"async/worker/handler.go", "modules/post", true},
		{"async/worker/handler.go", "modules/bus", false},
		{"platform/mysql.go", "modules/user", true},
		{"platform/postcache/post_body.go", "modules/post", false},
		{"platform/postcache/post_body.go", "modules/user", true},
		{"shared/apperror/error.go", "modules/user", true},
		{"transport/http/handlers/post/handler.go", "modules/post", false},
		{"transport/http/handlers/post/handler.go", "platform", true},
		{"transport/http/handlers/post/handler.go", "platform/config", true},
		{"modules/post/service.go", "testkit/integrationtest", true},
	} {
		if got := boundaryViolation(tc.source, internalPrefix+tc.target) != ""; got != tc.rejected {
			t.Errorf("依赖规则结果错误：%s -> %s，拒绝=%v", tc.source, tc.target, got)
		}
	}
	if boundaryViolation("modules/post/service.go", "github.com/gin-gonic/gin") == "" {
		t.Fatal("业务层的 Gin 依赖未被拒绝")
	}
}

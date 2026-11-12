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

// TestPackageBoundaries 防止业务处理重新依赖运行入口或通用平台依赖具体业务。
func TestPackageBoundaries(t *testing.T) {
	const prefix = "github.com/Ray-ymq/GoPulse/backend/internal/"
	domains := map[string]bool{"auth": true, "user": true, "post": true, "comment": true, "like": true, "bookmark": true, "notification": true, "search": true}
	foundations := map[string]bool{"platform": true, "config": true, "logging": true, "correlation": true, "apperror": true, "ratelimit": true, "resourcebudget": true, "bus": true}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source := strings.Split(filepath.ToSlash(path), "/")[0]
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(imported, prefix) {
				continue
			}
			target := strings.TrimPrefix(imported, prefix)
			targetRoot := strings.Split(target, "/")[0]
			if domains[source] && (target == "http" || targetRoot == "worker") {
				t.Errorf("业务包不得依赖 HTTP 组装或消费运行包：%s -> %s", path, target)
			}
			if foundations[source] && domains[targetRoot] {
				t.Errorf("公共能力不得依赖具体业务包：%s -> %s", path, target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

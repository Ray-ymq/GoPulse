package logging

import (
	"context"
	"strings"
	"testing"
)

func TestRequestIDGenerationValidationAndContext(t *testing.T) {
	first, err := NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidRequestID(first) || !ValidRequestID(second) || first == second {
		t.Fatal("请求 ID 无效或重复")
	}
	for _, value := range []string{"", first[:31], first + "0", strings.Repeat("A", 32), strings.Repeat("g", 32)} {
		if ValidRequestID(value) {
			t.Fatalf("错误接受请求 ID：%q", value)
		}
	}
	root := context.Background()
	if RequestID(root) != "" || RequestID(WithRequestID(root, first)) != first {
		t.Fatal("请求 ID 上下文隔离失败")
	}
}

package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const AttemptIDHeader = "x-gopulse-attempt-id"
const OutboxIDHeader = "x-gopulse-outbox-id"

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type attemptIDKey struct{}

type outboxIDKey struct{}

func WithAttemptID(ctx context.Context, id string) context.Context {
	if !validID(id) {
		return ctx
	}
	return context.WithValue(ctx, attemptIDKey{}, id)
}

func AttemptID(ctx context.Context) string {
	id, _ := ctx.Value(attemptIDKey{}).(string)
	if !validID(id) {
		return ""
	}
	return id
}

func WithOutboxID(ctx context.Context, id uint64) context.Context {
	if id == 0 {
		return ctx
	}
	return context.WithValue(ctx, outboxIDKey{}, id)
}

func OutboxID(ctx context.Context) uint64 {
	id, _ := ctx.Value(outboxIDKey{}).(uint64)
	return id
}

func NewAttemptID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	// 随机数读取失败时使用时间戳生成尝试编号，避免因此丢弃业务消息。
	return fmt.Sprintf("%032x", time.Now().UnixNano())
}

func validID(value string) bool {
	return idPattern.MatchString(value) && strings.Trim(value, "0") != ""
}

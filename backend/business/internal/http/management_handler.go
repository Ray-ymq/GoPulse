package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/request"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/user"
	"github.com/gin-gonic/gin"
)

type ManagementHandler struct {
	users *user.MySQLRepository
	key   [32]byte
}

func NewManagementHandler(users *user.MySQLRepository, secret string) *ManagementHandler {
	return &ManagementHandler{users: users, key: sha256.Sum256([]byte("gopulse/management-audit/v1\x00" + secret))}
}
func managementError(c *gin.Context, err error) {
	if errors.Is(err, user.ErrNotFound) {
		err = apperror.New(apperror.CodeUserNotFound, "user was not found")
	}
	response.Error(c, err)
}
func managementValidation() error {
	return apperror.New(apperror.CodeValidationFailed, "invalid management request")
}
func (h *ManagementHandler) User(c *gin.Context) {
	raw := c.Param("userId")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != raw {
		response.Error(c, managementValidation())
		return
	}
	if c.Request.Method == "GET" {
		record, err := h.users.ManagedByID(c.Request.Context(), id)
		if err != nil {
			managementError(c, err)
			return
		}
		response.Data(c, 200, record)
		return
	}
	var body struct {
		Role user.Role `json:"role"`
	}
	if err = request.DecodeJSON(c, &body); err != nil {
		response.Error(c, err)
		return
	}
	if _, err = user.ParseRole(string(body.Role)); err != nil {
		response.Error(c, managementValidation())
		return
	}
	actor, _ := middleware.CurrentUserID(c)
	result, err := h.users.ChangeRole(c.Request.Context(), actor, id, body.Role, c.Writer.Header().Get("X-Request-ID"))
	if err != nil {
		managementError(c, err)
		return
	}
	response.Data(c, 200, result)
}
func (h *ManagementHandler) sign(token string, actor uint64) string {
	mac := hmac.New(sha256.New, h.key[:])
	mac.Write([]byte(strconv.FormatUint(actor, 10) + "\x00" + token))
	return token + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (h *ManagementHandler) Audit(c *gin.Context) {
	q := user.AuditQuery{End: time.Now().UTC(), Limit: 50}
	q.Start = q.End.Add(-90 * 24 * time.Hour)
	actor, _ := middleware.CurrentUserID(c)
	values := c.Request.URL.Query()
	for key, v := range values {
		if len(v) != 1 {
			response.Error(c, managementValidation())
			return
		}
		switch key {
		case "cursor", "limit", "start", "end", "action", "resource_type", "outcome":
		default:
			response.Error(c, managementValidation())
			return
		}
	}
	if token := values.Get("cursor"); token != "" {
		parts := strings.Split(token, ".")
		if len(token) > 2048 || len(parts) != 2 || !hmac.Equal([]byte(h.sign(parts[0], actor)), []byte(token)) {
			response.Error(c, managementValidation())
			return
		}
		b, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil || json.Unmarshal(b, &q) != nil {
			response.Error(c, managementValidation())
			return
		}
		// Continuations preserve the signed query rather than accepting changed filters.
		if len(values) != 1 {
			response.Error(c, managementValidation())
			return
		}
	} else {
		var err error
		if s := values.Get("end"); s != "" {
			q.End, err = time.Parse(time.RFC3339Nano, s)
			if err != nil {
				response.Error(c, managementValidation())
				return
			}
			q.Start = q.End.Add(-90 * 24 * time.Hour)
		}
		if s := values.Get("start"); s != "" {
			q.Start, err = time.Parse(time.RFC3339Nano, s)
			if err != nil {
				response.Error(c, managementValidation())
				return
			}
		}
		if s := values.Get("limit"); s != "" {
			q.Limit, err = strconv.Atoi(s)
			if err != nil {
				response.Error(c, managementValidation())
				return
			}
		}
		q.Action = values.Get("action")
		q.Resource = values.Get("resource_type")
		q.Outcome = values.Get("outcome")
	}
	if q.Limit < 1 || q.Limit > 100 || q.Start.After(q.End) || q.End.Sub(q.Start) > 90*24*time.Hour || (q.Action != "" && !user.ValidAuditAction(q.Action)) || (q.Resource != "" && q.Resource != "user" && q.Resource != "rule" && q.Resource != "plugin" && q.Resource != "alert") || (q.Outcome != "" && q.Outcome != "succeeded" && q.Outcome != "failed" && q.Outcome != "unknown") {
		response.Error(c, managementValidation())
		return
	}
	rows, err := h.users.AuditEvents(c.Request.Context(), q)
	if err != nil {
		managementError(c, err)
		return
	}
	var next *string
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		q.Before = rows[len(rows)-1].ID
		b, _ := json.Marshal(q)
		s := h.sign(base64.RawURLEncoding.EncodeToString(b), actor)
		next = &s
	}
	response.Page(c, 200, rows, next)
}

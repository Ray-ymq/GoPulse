package alert

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/backend/internal/alert/count"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/middleware"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/metricquery"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Handler struct {
	repo *Repository
	key  [32]byte
}

func NewHandler(repo *Repository, secret string) *Handler {
	return &Handler{repo, sha256.Sum256([]byte("gopulse/alerts/v1\x00" + secret))}
}
func (h *Handler) Register(g *gin.RouterGroup) {
	g.Use(h.shape)
	g.GET("/catalog", h.catalog)
	g.GET("/rules", h.list)
	g.POST("/rules", h.mutate)
	g.GET("/rules/:ruleId", h.get)
	g.PUT("/rules/:ruleId", h.mutate)
	g.DELETE("/rules/:ruleId", h.mutate)
	g.POST("/rules/:ruleId/enable", h.mutate)
	g.POST("/rules/:ruleId/disable", h.mutate)
	g.GET("/current", h.list)
	g.GET("/history", h.list)
}
func (h *Handler) shape(c *gin.Context) {
	if c.Request.Method == "GET" {
		b, e := io.ReadAll(io.LimitReader(c.Request.Body, 1))
		if e != nil || len(b) > 0 {
			response.Error(c, validation())
			c.Abort()
			return
		}
	}
	if c.Request.Method != "GET" && c.Request.URL.RawQuery != "" {
		response.Error(c, validation())
		c.Abort()
		return
	}
	c.Next()
}

// Walk tokens before typed decoding: encoding/json alone accepts duplicate keys
// and replaces invalid UTF-8, which is not the management request contract.
func strict(raw []byte, dst any) error {
	if !utf8.Valid(raw) || len(raw) == 0 || len(raw) > 16384 {
		return validation()
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func() error
	walk = func() error {
		t, e := dec.Token()
		if e != nil {
			return e
		}
		d, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if d != '{' && d != '[' {
			return validation()
		}
		seen := map[string]bool{}
		for dec.More() {
			if d == '{' {
				k, e := dec.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return validation()
				}
				seen[key] = true
			}
			if e := walk(); e != nil {
				return e
			}
		}
		_, e = dec.Token()
		return e
	}
	if rawTrim := bytes.TrimSpace(raw); len(rawTrim) == 0 || rawTrim[0] != '{' {
		return validation()
	}
	if walk() != nil {
		return validation()
	}
	if _, e := dec.Token(); e != io.EOF {
		return validation()
	}
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil {
		return validation()
	}
	return nil
}
func body(c *gin.Context, dst any) error {
	raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 16384))
	if e != nil {
		return validation()
	}
	return strict(raw, dst)
}
func id(raw string) (uint64, error) {
	n, e := strconv.ParseUint(raw, 10, 64)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != raw {
		return 0, validation()
	}
	return n, nil
}
func (h *Handler) catalog(c *gin.Context) {
	if c.Request.URL.RawQuery != "" {
		response.Error(c, validation())
		return
	}
	response.Data(c, 200, struct {
		Logs       count.Catalog                 `json:"logs"`
		Events     count.Catalog                 `json:"events"`
		Sources    []string                      `json:"creatable_sources"`
		Metrics    []metricquery.AlertDefinition `json:"metrics"`
		Operators  []string                      `json:"operators"`
		Windows    []string                      `json:"windows"`
		For        []string                      `json:"for"`
		Severities []string                      `json:"severities"`
	}{logquery.AlertCatalog(), eventquery.AlertCatalog(), []string{"metrics", "logs", "events"}, metricquery.AlertCatalog(), []string{"gt", "gte", "lt", "lte", "eq", "neq"}, []string{"1m", "5m", "15m"}, []string{"0s", "1m", "5m"}, []string{"warning", "critical"}})
}
func (h *Handler) get(c *gin.Context) {
	n, e := id(c.Param("ruleId"))
	if e != nil || c.Request.URL.RawQuery != "" {
		response.Error(c, validation())
		return
	}
	r, e := h.repo.Get(c.Request.Context(), n)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Data(c, 200, r)
}
func (h *Handler) mutate(c *gin.Context) {
	action := "create"
	var n uint64
	var e error
	if raw := c.Param("ruleId"); raw != "" {
		n, e = id(raw)
		if e != nil {
			response.Error(c, e)
			return
		}
		switch c.Request.Method {
		case "PUT":
			action = "update"
		case "DELETE":
			action = "delete"
		default:
			action = "enable"
			if strings.HasSuffix(c.FullPath(), "/disable") {
				action = "disable"
			}
		}
	}
	var in Input
	if action == "create" || action == "update" {
		e = body(c, &in)
	} else {
		var b struct {
			Revision uint64 `json:"revision"`
		}
		e = body(c, &b)
		in.Revision = b.Revision
	}
	if e != nil {
		response.Error(c, e)
		return
	}
	actor, _ := middleware.CurrentUserID(c)
	r, e := h.repo.Mutate(c.Request.Context(), n, actor, action, c.Writer.Header().Get("X-Request-ID"), in)
	if e != nil {
		response.Error(c, e)
		return
	}
	if action == "delete" {
		c.Status(204)
		return
	}
	status := 200
	if action == "create" {
		status = 201
	}
	response.Data(c, status, r)
}
func (h *Handler) sign(token string, actor uint64) string {
	mac := hmac.New(sha256.New, h.key[:])
	mac.Write([]byte(strconv.FormatUint(actor, 10) + "\x00" + token))
	return token + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (h *Handler) list(c *gin.Context) {
	kind := c.FullPath()[strings.LastIndex(c.FullPath(), "/")+1:]
	q := Query{Kind: kind, Limit: 50, End: time.Now().UTC()}
	q.Start = q.End.Add(-90 * 24 * time.Hour)
	v, e := httpQuery(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	actor, _ := middleware.CurrentUserID(c)
	if token := v["cursor"]; token != "" {
		if len(v) != 1 || len(token) > 4096 {
			response.Error(c, validation())
			return
		}
		parts := strings.Split(token, ".")
		if len(parts) != 2 || !hmac.Equal([]byte(token), []byte(h.sign(parts[0], actor))) {
			response.Error(c, validation())
			return
		}
		raw, e := base64.RawURLEncoding.DecodeString(parts[0])
		if e != nil || json.Unmarshal(raw, &q) != nil || q.Kind != kind {
			response.Error(c, validation())
			return
		}
	} else {
		for k, val := range v {
			switch k {
			case "limit":
				q.Limit, e = strconv.Atoi(val)
			case "source":
				q.Source = val
			case "status":
				q.Status = val
			case "severity":
				q.Severity = val
			case "rule":
				q.RuleID, e = id(val)
			case "start":
				q.Start, e = time.Parse(time.RFC3339Nano, val)
			case "end":
				q.End, e = time.Parse(time.RFC3339Nano, val)
			default:
				e = validation()
			}
			if e != nil {
				response.Error(c, validation())
				return
			}
		}
		if q.Limit < 1 || q.Limit > 100 || q.Source != "" && q.Source != "metrics" || !member(q.Severity, "", "warning", "critical") || q.Start.After(q.End) || q.End.Sub(q.Start) > 90*24*time.Hour {
			response.Error(c, validation())
			return
		}
		if kind == "rules" {
			if !member(q.Status, "", "normal", "pending", "firing", "disabled") || q.Severity != "" || q.RuleID != 0 || v["start"] != "" || v["end"] != "" {
				response.Error(c, validation())
				return
			}
		} else if !member(q.Status, "", "firing", "recovered", "closed") || kind == "current" && (q.Status != "" || q.RuleID != 0 || v["start"] != "" || v["end"] != "") {
			response.Error(c, validation())
			return
		}
	}
	var data any
	more := false
	if kind == "rules" {
		rows, err := h.repo.ListRules(c.Request.Context(), q)
		e = err
		if len(rows) > q.Limit {
			rows = rows[:q.Limit]
			more = true
			q.Before = rows[len(rows)-1].ID
		}
		data = rows
	} else {
		rows, err := h.repo.ListIncidents(c.Request.Context(), q)
		e = err
		if len(rows) > q.Limit {
			rows = rows[:q.Limit]
			more = true
			r := rows[len(rows)-1]
			q.Before = r.ID
			q.Time = r.FirstTriggeredAt
			q.Rank = 0
			if r.Severity == "critical" {
				q.Rank = 1
			}
		}
		data = rows
	}
	if e != nil {
		response.Error(c, e)
		return
	}
	var next *string
	if more {
		raw, _ := json.Marshal(q)
		token := h.sign(base64.RawURLEncoding.EncodeToString(raw), actor)
		next = &token
	}
	response.Page(c, 200, data, next)
}
func httpQuery(c *gin.Context) (map[string]string, error) {
	raw, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil {
		return nil, validation()
	}
	v := map[string]string{}
	for k, a := range raw {
		if len(a) != 1 || a[0] == "" {
			return nil, validation()
		}
		v[k] = a[0]
	}
	return v, nil
}

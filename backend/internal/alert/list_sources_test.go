package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// This test requires a disposable, explicitly named Review database.
func TestReviewMySQLSourceLists(t *testing.T) {
	dsn := os.Getenv("REVIEW_ALERT_DSN")
	if dsn == "" {
		t.Skip("requires disposable Review MySQL")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName != "gopulse_review_151207" {
		t.Fatal("refusing non-review database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES(151207,'review-list','not-a-login','super_admin')`)
	exec(`INSERT INTO bootstrap_super_admin(singleton,user_id) VALUES(1,151207)`)
	defer func() {
		exec(`DELETE FROM management_audit_events WHERE resource_type='rule'`)
		exec(`DELETE FROM alert_rule_states`)
		exec(`DELETE FROM alert_incidents`)
		exec(`DELETE FROM alert_rules`)
		exec(`DELETE FROM bootstrap_super_admin WHERE user_id=151207`)
		exec(`DELETE FROM users WHERE id=151207`)
	}()
	repo := NewRepository(db)
	for _, source := range []string{"metrics", "logs", "events"} {
		for i := 0; i < 2; i++ {
			in := validInput()
			in.Source = source
			in.Name = source + strings.Repeat("x", i+1)
			if source != "metrics" {
				in.Reducer = "count"
				in.Selector = Selector{Labels: map[string]string{"severity": "error"}}
				if source == "logs" {
					in.Selector.Labels = map[string]string{"level": "error"}
				}
			}
			r, e := repo.Mutate(context.Background(), 0, 151207, "create", "", in)
			if e != nil {
				t.Fatal(e)
			}
			object, _ := json.Marshal(r.Selector)
			exec(`INSERT INTO alert_incidents(rule_id,revision,name,severity,source,object,status,first_triggered_at,last_triggered_at,last_evaluated_at,evaluation_count) VALUES(?,?,?,?,?,?,'firing',?,?,?,1)`, r.ID, r.Revision, r.Name, r.Severity, source, object, time.Now().UTC(), time.Now().UTC(), time.Now().UTC())
		}
	}
	g := gin.New()
	NewHandler(repo, "review-cursor-key").Register(g.Group("/alerts"))
	for _, kind := range []string{"rules", "current", "history"} {
		t.Run(kind, func(t *testing.T) {
			for _, source := range []string{"metrics", "logs", "events"} {
				path := "/alerts/" + kind + "?source=" + source + "&limit=1"
				seen := map[uint64]bool{}
				for page := 0; page < 2; page++ {
					w := httptest.NewRecorder()
					g.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
					if w.Code != 200 {
						t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
					}
					var body struct {
						Data []struct {
							ID     uint64
							Source string
						}
						Meta struct {
							Next *string `json:"next_cursor"`
						}
					}
					if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
						t.Fatal(e)
					}
					if len(body.Data) != 1 || body.Data[0].Source != source || seen[body.Data[0].ID] {
						t.Fatalf("lost source/page: %s", w.Body.String())
					}
					seen[body.Data[0].ID] = true
					if page == 0 {
						if body.Meta.Next == nil {
							t.Fatal("missing cursor")
						}
						path = "/alerts/" + kind + "?cursor=" + url.QueryEscape(*body.Meta.Next)
						bad := httptest.NewRecorder()
						g.ServeHTTP(bad, httptest.NewRequest("GET", path+"x", nil))
						if bad.Code != 400 {
							t.Fatal("tampered cursor accepted")
						}
					} else if body.Meta.Next != nil {
						t.Fatal("unexpected third page")
					}
				}
			}
			w := httptest.NewRecorder()
			g.ServeHTTP(w, httptest.NewRequest("GET", "/alerts/"+kind+"?source=unknown", nil))
			if w.Code != 400 {
				t.Fatal("unknown source accepted")
			}
		})
	}
}

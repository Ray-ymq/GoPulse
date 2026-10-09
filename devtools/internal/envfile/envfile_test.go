package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestComposeKeepsDefaultsFileAndCallerPrecedence(t *testing.T) {
	root := fixture(t, map[string]string{
		".env.example": "HTTP_PORT=8080\nSHARED=example\nQUOTED=\"quoted value\"\n",
		".env":         "HTTP_PORT=8081\nSHARED=file\nLITERAL=$SHARED-not-expanded\n",
	})
	values, err := Compose(root, ModeDev, "", map[string]string{"HTTP_PORT": "9090"})
	if err != nil {
		t.Fatal(err)
	}
	if values["HTTP_PORT"] != "9090" {
		t.Errorf("a caller variable must win over the file and the default: got %q", values["HTTP_PORT"])
	}
	if values["SHARED"] != "file" {
		t.Errorf("the file must win over the checked-in default: got %q", values["SHARED"])
	}
	if values["QUOTED"] != "quoted value" {
		t.Errorf("quoted value must lose its quotes: got %q", values["QUOTED"])
	}
	if values["LITERAL"] != "$SHARED-not-expanded" {
		t.Errorf("values must not be expanded: got %q", values["LITERAL"])
	}
	if values["APP_ENV"] != "development" {
		t.Errorf("dev mode default must apply: got %q", values["APP_ENV"])
	}
}

func TestComposeKeepsCallerOverrideOverModeDefault(t *testing.T) {
	root := fixture(t, map[string]string{
		".env.example": "HTTP_PORT=8080\nALERT_EVALUATION_ENABLED=false\n",
	})
	values, err := Compose(root, ModeDev, "", map[string]string{
		"HTTP_PORT":                "9999",
		"ALERT_EVALUATION_ENABLED": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if values["HTTP_PORT"] != "9999" || values["ALERT_EVALUATION_ENABLED"] != "true" {
		t.Errorf("caller variables must not be replaced by mode defaults: %v", values)
	}
}

func TestExplicitEnvironmentFileMustExist(t *testing.T) {
	root := fixture(t, map[string]string{".env.example": "HTTP_PORT=8080\n"})
	if _, err := Compose(root, ModeDev, filepath.Join(root, "missing.env"), nil); err == nil {
		t.Fatal("a missing explicit environment file must fail")
	} else if !strings.Contains(err.Error(), "environment file does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTestModeForcesIsolationOverCallerAndFile(t *testing.T) {
	// A developer's .env must never leak into an isolated test stack.
	root := fixture(t, map[string]string{
		".env.example": "HTTP_PORT=8080\nMYSQL_PORT=3306\nMYSQL_USER=gopulse\nMYSQL_DATABASE=gopulse\nREDIS_PORT=6379\nREDIS_DB=0\n",
		".env":         "MYSQL_PORT=3306\nMYSQL_USER=gopulse\nMYSQL_DATABASE=gopulse\nREDIS_DB=0\nAPP_ENV=development\n",
	})
	values, err := Compose(root, ModeTest, "", map[string]string{
		"MYSQL_PORT":     "3306",
		"MYSQL_DATABASE": "gopulse",
		"REDIS_DB":       "0",
		"APP_ENV":        "development",
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"MYSQL_PORT":                "23306",
		"MYSQL_USER":                "gopulse_integration",
		"MYSQL_DATABASE":            "gopulse_integration",
		"MYSQL_PASSWORD":            "integration-mysql",
		"REDIS_PORT":                "26379",
		"REDIS_DB":                  "15",
		"APP_ENV":                   "test",
		"HTTP_PORT":                 "18080",
		"FRONTEND_PORT":             "15173",
		"ADMIN_FRONTEND_PORT":       "15174",
		"ELASTICSEARCH_PORT":        "29200",
		"RABBITMQ_PORT":             "25672",
		"VICTORIAMETRICS_PORT":      "18428",
		"MONITOR_HTTP_PORT":         "19090",
		"ROUTER_HTTP_PORT":          "19091",
		"MARSHALLER_HTTP_PORT":      "19093",
		"REDIS_EXPORTER_HTTP_PORT":  "19121",
		"AUTH_COOKIE_NAME":          "gopulse_test_session",
		"BACKEND_ENDPOINTS":         "backend",
		"BUSINESS_WORKER_ENDPOINTS": "business-worker",
		"SEARCH_INDEXER_ENDPOINTS":  "search-indexer",
		"ROUTER_ENDPOINTS":          "router",
		"MARSHALLER_ENDPOINTS":      "marshaller",
		"MONITOR_BOOTSTRAP_PACKAGE": "/opt/gopulse/packages/gopulse-redis-exporter.tar.gz",
	}
	for key, want := range expected {
		if values[key] != want {
			t.Errorf("test mode %s = %q, want %q", key, values[key], want)
		}
	}
}

func TestTestModeUsesItsOwnPortsAndDatabase(t *testing.T) {
	root := fixture(t, map[string]string{".env.example": "HTTP_PORT=8080\nMYSQL_PORT=3306\nREDIS_PORT=6379\n"})
	development, err := Compose(root, ModeDev, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	test, err := Compose(root, ModeTest, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"HTTP_PORT", "MYSQL_PORT", "REDIS_PORT"} {
		if development[key] == test[key] {
			t.Errorf("%s must differ between development and test: both %q", key, development[key])
		}
	}
}

func TestUnknownModeIsRejected(t *testing.T) {
	root := fixture(t, map[string]string{".env.example": "HTTP_PORT=8080\n"})
	if _, err := Compose(root, "staging", "", nil); err == nil {
		t.Fatal("an unknown environment mode must fail")
	}
}

func TestWritePrivateSortsKeysAndRestrictsPermissions(t *testing.T) {
	private := filepath.Join(t.TempDir(), "private")
	path, err := WritePrivate(private, ModeDev, Values{
		"HTTP_PORT": "8080",
		"APP_ENV":   "development",
		"not-a-key": "dropped",
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private environment file mode = %v, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "APP_ENV=development\nHTTP_PORT=8080\n" {
		t.Errorf("private environment file content = %q", string(raw))
	}
}

func TestInvalidEnvironmentEntryIsRejected(t *testing.T) {
	root := fixture(t, map[string]string{
		".env.example": "HTTP_PORT=8080\n",
		".env":         "INVALID LINE\n",
	})
	if _, err := Compose(root, ModeDev, "", nil); err == nil {
		t.Fatal("an invalid environment entry must fail")
	}
}

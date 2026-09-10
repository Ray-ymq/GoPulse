package plugin

import (
	"strings"
	"testing"
)

func TestRedisConfigurationRequest(t *testing.T) {
	const config = `{"host":"redis","port":6379,"database":0,"connect_timeout":"1s","scrape_timeout":"2s"}`
	const secret = "phase14-request-secret-canary"
	request := `{"config":` + config + `,"secrets":{"password":"` + secret + `"}}`
	got, credential, err := ParseRedisConfigurationRequest([]byte(request), "container", nil)
	if err != nil || got.Host != "redis" || credential.Password != secret {
		t.Fatal("valid request rejected", err)
	}
	for _, preserve := range []string{`{"config":` + config + `}`, `{"config":` + config + `,"secrets":{}}`} {
		_, retained, err := ParseRedisConfigurationRequest([]byte(preserve), "container", &credential)
		if err != nil || retained != credential {
			t.Fatal("replacement failed to preserve secret", err)
		}
		if _, _, err := ParseRedisConfigurationRequest([]byte(preserve), "container", nil); err == nil {
			t.Fatal("initial request borrowed secret")
		}
	}
	for _, bad := range []string{
		config, `null`, `[]`, `{"config":null,"secrets":{}}`,
		`{"config":` + config + `,"secrets":null}`,
		`{"config":` + config + `,"secrets":[]}`,
		strings.Replace(request, `"secrets":`, `"unknown":0,"secrets":`, 1),
		strings.Replace(request, `"host":"redis"`, `"host":"redis","password":"`+secret+`"`, 1),
		strings.Replace(request, `"password":`, `"username":"unused","password":`, 1),
		strings.Replace(request, `"password":`, `"password":"duplicate","password":`, 1),
		strings.Replace(request, `"`+secret+`"`, `null`, 1),
		strings.Replace(request, `"`+secret+`"`, `""`, 1),
		request + strings.Repeat(" ", 16<<10),
	} {
		cfg, sec, err := ParseRedisConfigurationRequest([]byte(bad), "container", &credential)
		if err == nil {
			t.Fatal("invalid request accepted")
		}
		if cfg != (RedisConfig{}) || sec != (RedisSecret{}) || strings.Contains(err.Error(), secret) {
			t.Fatal("candidate leaked")
		}
	}
}

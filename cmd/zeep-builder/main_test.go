package main

import (
	"strings"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const testSecret = "0123456789abcdef0123456789abcdef"

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(envMap(map[string]string{"BUILDER_SHARED_SECRET": testSecret}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != ":8081" || cfg.workDir != "/work" || cfg.templatesDir != "/opt/templates" ||
		cfg.maxSessions != 4 || cfg.npmCache != "/work/.npm-cache" || cfg.home != "" || cfg.npmRegistry != "" {
		t.Fatalf("unexpected defaults: addr=%q work=%q tpl=%q max=%d cache=%q", cfg.addr, cfg.workDir, cfg.templatesDir, cfg.maxSessions, cfg.npmCache)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	cfg, err := loadConfig(envMap(map[string]string{
		"BUILDER_SHARED_SECRET":       testSecret,
		"BUILDER_ADDR":                "127.0.0.1:9000",
		"BUILDER_WORK_DIR":            "/data/work",
		"BUILDER_TEMPLATES_DIR":       "/data/tpl",
		"BUILDER_MAX_ACTIVE_SESSIONS": "7",
		"BUILDER_NPM_REGISTRY":        "http://registry-proxy:4873/",
		"BUILDER_NPM_CACHE":           "/data/cache",
		"BUILDER_HOME":                "/home/builder",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != "127.0.0.1:9000" || cfg.workDir != "/data/work" || cfg.templatesDir != "/data/tpl" ||
		cfg.maxSessions != 7 || cfg.npmRegistry != "http://registry-proxy:4873/" ||
		cfg.npmCache != "/data/cache" || cfg.home != "/home/builder" {
		t.Fatalf("overrides not applied: %+v", cfg.addr)
	}
}

func TestLoadConfigRejectsBadValuesWithoutEchoingThem(t *testing.T) {
	base := func() map[string]string { return map[string]string{"BUILDER_SHARED_SECRET": testSecret} }
	tests := []struct {
		name string
		set  func(m map[string]string)
		want string
	}{
		{"missing secret", func(m map[string]string) { delete(m, "BUILDER_SHARED_SECRET") }, "BUILDER_SHARED_SECRET is required"},
		{"short secret", func(m map[string]string) { m["BUILDER_SHARED_SECRET"] = "short-secret" }, "at least"},
		{"capacity zero", func(m map[string]string) { m["BUILDER_MAX_ACTIVE_SESSIONS"] = "0" }, "BUILDER_MAX_ACTIVE_SESSIONS"},
		{"capacity text", func(m map[string]string) { m["BUILDER_MAX_ACTIVE_SESSIONS"] = "many" }, "BUILDER_MAX_ACTIVE_SESSIONS"},
		{"registry scheme", func(m map[string]string) { m["BUILDER_NPM_REGISTRY"] = "ftp://user:hunter2@host/" }, "BUILDER_NPM_REGISTRY"},
		{"relative home", func(m map[string]string) { m["BUILDER_HOME"] = "relative" }, "BUILDER_HOME"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.set(m)
			_, err := loadConfig(envMap(m))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
			for _, secret := range []string{testSecret, m["BUILDER_SHARED_SECRET"], "hunter2"} {
				if secret != "" && strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks a value: %q", err)
				}
			}
		})
	}
}

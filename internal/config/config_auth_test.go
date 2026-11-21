package config

import (
	"strings"
	"testing"
	"time"
)

func TestAuthAndDatabaseDefaults(t *testing.T) {
	cfg, err := FromMap(nil)
	if err != nil {
		t.Fatalf("defaults must be valid: %v", err)
	}
	if cfg.AdminUsername != DefaultAdminUsername || cfg.AdminPassword != DefaultAdminPassword {
		t.Errorf("admin defaults = %q/%q", cfg.AdminUsername, cfg.AdminPassword)
	}
	if cfg.DBPath == "" || cfg.SessionTTL <= 0 {
		t.Errorf("db/session defaults = %q/%s", cfg.DBPath, cfg.SessionTTL)
	}
	if !cfg.UsingDefaultCredentials() {
		t.Error("defaults should be flagged as default credentials")
	}
}

func TestAuthAndDatabaseOverrides(t *testing.T) {
	cfg, err := FromMap(map[string]string{
		VarAdminUsername: "jerry",
		VarAdminPassword: "a-longer-password",
		VarAuthSecret:    "a-secret-that-is-long-enough",
		VarDBPath:        "/var/lib/splash/db.sqlite",
		VarCookieSecure:  "true",
		VarSessionTTL:    "24h",
		VarSeedDemo:      "true",
	})
	if err != nil {
		t.Fatalf("FromMap: %v", err)
	}
	if cfg.AdminUsername != "jerry" || cfg.DBPath != "/var/lib/splash/db.sqlite" ||
		!cfg.CookieSecure || cfg.SessionTTL != 24*time.Hour || !cfg.SeedDemo {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.UsingDefaultCredentials() {
		t.Error("custom credentials should not be flagged as defaults")
	}
}

func TestAuthValidation(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"short password", map[string]string{VarAdminPassword: "short"}, VarAdminPassword},
		{"short secret", map[string]string{VarAuthSecret: "tiny"}, VarAuthSecret},
		{"empty username", map[string]string{VarAdminUsername: "   "}, ""}, // blank falls back to the default
		{"bad session ttl", map[string]string{VarSessionTTL: "5m"}, VarSessionTTL},
		{"huge session ttl", map[string]string{VarSessionTTL: "9000h"}, VarSessionTTL},
		{"bad cookie secure", map[string]string{VarCookieSecure: "yes"}, VarCookieSecure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FromMap(tt.vars)
			if tt.want == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to name %s", err, tt.want)
			}
		})
	}
}

func TestLogValueRedactsSecrets(t *testing.T) {
	cfg := Default()
	cfg.AdminPassword = "super-secret-password"
	cfg.AuthSecret = "super-secret-signing-key"
	rendered := cfg.LogValue().String()
	if strings.Contains(rendered, "super-secret-password") || strings.Contains(rendered, "super-secret-signing-key") {
		t.Errorf("LogValue leaked a secret: %s", rendered)
	}
	if !strings.Contains(rendered, "dbPath") || !strings.Contains(rendered, "adminUsername") {
		t.Errorf("LogValue should describe the non-secret auth config: %s", rendered)
	}
}

func TestOverridesApplyToDatabaseAndSeed(t *testing.T) {
	cfg := Default()
	seed := true
	if err := cfg.Apply(Overrides{DBPath: "/tmp/other.db", SeedDemo: &seed}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if cfg.DBPath != "/tmp/other.db" || !cfg.SeedDemo {
		t.Errorf("cfg = %+v", cfg)
	}
	// An unspecified flag leaves the environment value alone.
	if err := cfg.Apply(Overrides{}); err != nil {
		t.Fatalf("Apply empty: %v", err)
	}
	if cfg.DBPath != "/tmp/other.db" || !cfg.SeedDemo {
		t.Error("empty overrides changed the configuration")
	}
}

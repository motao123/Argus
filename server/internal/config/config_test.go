package config

import "testing"

func TestValidateStartupRejectsWeakProductionCredentials(t *testing.T) {
	for _, pass := range []string{"", "argus123", "change-me-before-production", "short"} {
		c := &Config{AdminUser: "admin", AdminPass: pass}
		if err := c.ValidateStartup(); err == nil {
			t.Fatalf("password %q should be rejected", pass)
		}
	}
}

func TestValidateStartupAllowsExplicitDevelopmentMode(t *testing.T) {
	c := &Config{AdminUser: "admin", DevMode: true}
	if err := c.ValidateStartup(); err != nil {
		t.Fatal(err)
	}
	if c.AdminPass != "argus123" {
		t.Fatalf("dev password = %q", c.AdminPass)
	}
}

func TestValidateStartupRejectsShortJWTSecret(t *testing.T) {
	c := &Config{AdminUser: "admin", AdminPass: "correct horse battery staple", JWTSecret: "short"}
	if err := c.ValidateStartup(); err == nil {
		t.Fatal("short JWT secret should be rejected")
	}
}

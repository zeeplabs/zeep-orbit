package config

import (
	"os"
	"strings"
)

type GoogleOAuthConfig struct {
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	AllowedDomains []string
}

func LoadGoogleOAuthConfig() *GoogleOAuthConfig {
	raw := os.Getenv("GOOGLE_ALLOWED_DOMAINS")
	var domains []string
	if raw != "" {
		for _, d := range strings.Split(raw, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				domains = append(domains, d)
			}
		}
	}
	return &GoogleOAuthConfig{
		ClientID:       os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret:   os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:    os.Getenv("GOOGLE_REDIRECT_URL"),
		AllowedDomains: domains,
	}
}

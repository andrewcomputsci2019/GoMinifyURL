package utils

import (
	"net/url"
	"strings"
)

func IsValidIssuerUrl(rawURL string) bool {
	u, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	// Must be absolute and use HTTPS
	return u.Scheme == "https" && u.Host != ""
}

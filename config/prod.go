//go:build prod

package config

import "os"

const (
	SSL = true
)

func AllowedOrigins() []string {
	return []string{
		"https://" + os.Getenv("DOMAIN"),
	}
}

func AllowedHosts() []string {
	return []string{
		os.Getenv("DOMAIN")
	}
}

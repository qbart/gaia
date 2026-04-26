//go:build !prod && !beta

package config

const (
	SSL = false
)

func AllowedOrigins() []string {
	// Cannot use "*" with credentials - must specify exact origins
	return []string{"http://localhost:4000"}
}

func AllowedHosts() []string {
	return []string{"localhost:4000"}
}

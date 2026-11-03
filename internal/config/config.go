// Package config loads the small set of runtime settings zanime reads from
// the environment (and .env).
package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// LoadDotEnv membaca KEY=VALUE dari path lewat godotenv. Variabel yang sudah
// ada di environment menang (godotenv tidak menimpanya), dan berkas yang tidak
// ada bukan error.
func LoadDotEnv(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return godotenv.Load(path)
}

// Int returns a positive integer from the environment, or fallback.
func Int(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// String returns a non-empty environment value, or fallback.
func String(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// ProxyURL returns PROXY_URL (mis. http://user:pass@host:80), atau "" bila
// kosong — artinya koneksi langsung tanpa proxy.
func ProxyURL() string { return String("PROXY_URL", "") }

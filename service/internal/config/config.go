// Package config reads the service's settings from the environment.
//
// Every setting is an environment variable because that is how the host hands
// secrets to containers (/srv/www/.env.prod, format: raw). There is no config
// file and no library: a missing required value is a startup error naming it.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string
	Listen      string
	LogLevel    string

	// Web role.
	WallRoot       string   // where wall/<public_id>/<variant>.jpg live; Rails' cable reads the same tree
	WallGrantKey   []byte   // Rails' key_generator.generate_key("wall_grant"), 64 bytes
	CameraTokenKey string   // Rails' secret_key_base, raw: camera tokens are HMAC-SHA256 over it
	MACBlacklist   []string // SNAPSHOT_MAC_BLACKLIST
	IPWhitelist    []string // SNAPSHOT_IP_WHITELIST
	VariantWorkers int
	VipsBin        string
	VipsHeaderBin  string
	Shadow         bool // a mirror of production traffic: log decisions, write only its own store
	GrantsDisabled bool // WALL_GRANTS_DISABLED=1: the frame socket serves without grants (the emergency switch)
	SnapshotMaxAge time.Duration

	// Firmware role.
	CatalogueDir        string
	ReleaseCacheRoot    string // tarballs, keyed by digest
	FirmwareCacheRoot   string // assembled images
	FirmwareAccelPrefix string // the nginx internal location that aliases FirmwareCacheRoot
	FirmwareCacheMax    int64  // bytes; a backstop, not the eviction policy
	BuildsPerMinute     int
	DownloadBase        string
}

// Load reads everything; Require checks what a given command needs.
func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		Listen:              os.Getenv("LISTEN"),
		LogLevel:            str("LOG_LEVEL", "info"),
		WallRoot:            str("WALL_ROOT", "/srv/wall"),
		CameraTokenKey:      os.Getenv("CAMERA_TOKEN_KEY"),
		MACBlacklist:        list("SNAPSHOT_MAC_BLACKLIST"),
		IPWhitelist:         list("SNAPSHOT_IP_WHITELIST"),
		VariantWorkers:      num("VARIANT_WORKERS", 2),
		VipsBin:             str("VIPS_BIN", "vips"),
		VipsHeaderBin:       str("VIPSHEADER_BIN", "vipsheader"),
		Shadow:              os.Getenv("SHADOW") == "1",
		GrantsDisabled:      os.Getenv("WALL_GRANTS_DISABLED") == "1",
		SnapshotMaxAge:      48 * time.Hour,
		CatalogueDir:        str("CATALOGUE_DIR", "/app/catalogue"),
		ReleaseCacheRoot:    str("RELEASE_CACHE_ROOT", "/srv/release-cache"),
		FirmwareCacheRoot:   str("FIRMWARE_CACHE_ROOT", "/srv/firmware"),
		FirmwareAccelPrefix: str("FIRMWARE_ACCEL_PREFIX", "/firmware-cache/"),
		FirmwareCacheMax:    int64(num("FIRMWARE_CACHE_MAX_MB", 4096)) << 20,
		BuildsPerMinute:     num("FIRMWARE_BUILDS_PER_MINUTE", 6),
		DownloadBase:        str("RELEASE_DOWNLOAD_BASE", "https://github.com/OpenIPC/firmware/releases/download"),
	}
	if raw := os.Getenv("WALL_GRANT_KEY"); raw != "" {
		key, err := hex.DecodeString(strings.TrimSpace(raw))
		if err != nil || len(key) != 64 {
			return nil, fmt.Errorf("WALL_GRANT_KEY must be 128 hex characters (64 bytes)")
		}
		c.WallGrantKey = key
	}
	return c, nil
}

// RequireDatabase is for every command that touches PostgreSQL.
func (c *Config) RequireDatabase() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}
	return nil
}

// RequireWeb is what `serve --role web` cannot start without. A missing grant
// key would mint grants Rails' cable refuses -- a blank wall with no error
// anywhere -- so it is a startup failure, not a default.
func (c *Config) RequireWeb() error {
	if err := c.RequireDatabase(); err != nil {
		return err
	}
	if c.WallGrantKey == nil {
		return fmt.Errorf("WALL_GRANT_KEY is not set")
	}
	if c.CameraTokenKey == "" {
		return fmt.Errorf("CAMERA_TOKEN_KEY is not set")
	}
	return nil
}

func str(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func num(name string, fallback int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
		return v
	}
	return fallback
}

// list reads a comma-separated list. Set but empty is an empty list, which is
// how a test server says "nothing here".
func list(name string) []string {
	var out []string
	for _, item := range strings.Split(os.Getenv(name), ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

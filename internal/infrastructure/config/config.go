package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server        ServerConfig
	Database      DatabaseConfig
	Auth          AuthConfig
	ObjectStorage ObjectStorageConfig
}

type ServerConfig struct {
	Port              string
	Mode              string
	TLSCertFile       string
	TLSKeyFile        string
	TrustedProxyCIDRs []string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
	// TimeZone is the plant's local zone (APP_TIMEZONE). Shift windows are
	// evaluated in it, and photo EXIF times without an offset are read in it.
	TimeZone string
}

type AuthConfig struct {
	AccessTokenDuration  time.Duration
	RefreshTokenDuration time.Duration
	BcryptCost           int
	ActiveKeyID          string
	JWTKeys              map[string][]byte
}

type ObjectStorageConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func (c ObjectStorageConfig) Configured() bool {
	return c.Endpoint != "" || c.AccessKey != "" || c.SecretKey != "" || c.Bucket != ""
}

func (c ObjectStorageConfig) Validate() error {
	if !c.Configured() {
		return nil
	}
	if c.Endpoint == "" || c.AccessKey == "" || c.SecretKey == "" || c.Bucket == "" {
		return fmt.Errorf("MINIO_ENDPOINT, MINIO_ACCESS_KEY, MINIO_SECRET_KEY and MINIO_BUCKET must be configured together")
	}
	if value, exists := os.LookupEnv("MINIO_USE_SSL"); exists {
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("MINIO_USE_SSL must be a boolean")
		}
	}
	return nil
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port:              getEnv("SERVER_PORT", "8080"),
			Mode:              getEnv("GIN_MODE", "debug"),
			TLSCertFile:       getEnv("TLS_CERT_FILE", ""),
			TLSKeyFile:        getEnv("TLS_KEY_FILE", ""),
			TrustedProxyCIDRs: getStringListEnv("TRUSTED_PROXY_CIDRS"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			DBName:   getEnv("DB_NAME", "auth_db"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
			TimeZone: getEnv("APP_TIMEZONE", "Asia/Jakarta"),
		},
		Auth: AuthConfig{
			AccessTokenDuration:  getDurationEnv("AUTH_ACCESS_TOKEN_DURATION", 15*time.Minute),
			RefreshTokenDuration: getDurationEnv("AUTH_REFRESH_TOKEN_DURATION", 7*24*time.Hour),
			BcryptCost:           getIntEnv("AUTH_BCRYPT_COST", 12),
			ActiveKeyID:          getEnv("JWT_ACTIVE_KID", "current"),
			JWTKeys:              getJWTKeys(),
		},
		ObjectStorage: ObjectStorageConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", ""),
			AccessKey: getEnv("MINIO_ACCESS_KEY", ""),
			SecretKey: getEnv("MINIO_SECRET_KEY", ""),
			Bucket:    getEnv("MINIO_BUCKET", ""),
			UseSSL:    getBoolEnv("MINIO_USE_SSL", false),
		},
	}
}

func (c ServerConfig) Validate() error {
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("TLS_CERT_FILE and TLS_KEY_FILE must be configured together")
	}
	if c.Mode == "release" && c.TLSCertFile == "" && len(c.TrustedProxyCIDRs) == 0 {
		return fmt.Errorf("release mode requires TLS_CERT_FILE/TLS_KEY_FILE or TRUSTED_PROXY_CIDRS for HTTPS termination")
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
	}
	return nil
}

func (c AuthConfig) Validate() error {
	if c.AccessTokenDuration <= 0 || c.RefreshTokenDuration <= 0 {
		return fmt.Errorf("access and refresh token durations must be positive")
	}
	if c.BcryptCost < 12 || c.BcryptCost > 31 {
		return fmt.Errorf("AUTH_BCRYPT_COST must be between 12 and 31")
	}
	for id, key := range c.JWTKeys {
		if strings.TrimSpace(id) == "" || len(key) < 32 {
			return fmt.Errorf("JWT key ids must be non-empty and keys must contain at least 32 bytes")
		}
	}
	if _, ok := c.JWTKeys[c.ActiveKeyID]; !ok {
		return fmt.Errorf("JWT_ACTIVE_KID %q does not name a configured JWT key", c.ActiveKeyID)
	}
	return nil
}

func getJWTKeys() map[string][]byte {
	keys := make(map[string][]byte)
	configured := os.Getenv("JWT_KEYS")
	if configured != "" {
		for _, entry := range strings.Split(configured, ";") {
			keyID, secret, ok := strings.Cut(entry, "=")
			if ok {
				keys[strings.TrimSpace(keyID)] = []byte(strings.TrimSpace(secret))
			}
		}
		return keys
	}
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		keys[getEnv("JWT_ACTIVE_KID", "current")] = []byte(secret)
	}
	return keys
}

func getStringListEnv(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getIntEnv(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

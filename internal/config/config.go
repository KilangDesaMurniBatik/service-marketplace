package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds all configuration for the marketplace service
type Config struct {
	App      AppConfig      `mapstructure:"app"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	NATS     NATSConfig     `mapstructure:"nats"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Sentry   SentryConfig   `mapstructure:"sentry"`
	Shopee   ShopeeConfig   `mapstructure:"shopee"`
	TikTok   TikTokConfig   `mapstructure:"tiktok"`
	Security SecurityConfig `mapstructure:"security"`
	Services ServicesConfig `mapstructure:"services"`
}

// RedisConfig holds Redis cache configuration
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// AppConfig holds application configuration
type AppConfig struct {
	Name string `mapstructure:"name"`
	Env  string `mapstructure:"env"`
	Port string `mapstructure:"port"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     string `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"name"`
	SSLMode  string `mapstructure:"ssl_mode"`
}

// NATSConfig holds NATS configuration
type NATSConfig struct {
	URL string `mapstructure:"url"`
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret string `mapstructure:"secret"`
}

// SentryConfig holds Sentry error tracking configuration
type SentryConfig struct {
	DSN         string `mapstructure:"dsn"`
	Environment string `mapstructure:"environment"`
	Release     string `mapstructure:"release"`
}

// ShopeeConfig holds Shopee Open Platform configuration
type ShopeeConfig struct {
	PartnerID   string `mapstructure:"partner_id"`
	PartnerKey  string `mapstructure:"partner_key"`
	RedirectURL string `mapstructure:"redirect_url"`
	IsSandbox   bool   `mapstructure:"is_sandbox"`
}

// TikTokConfig holds TikTok Shop Partner API configuration
type TikTokConfig struct {
	AppKey      string `mapstructure:"app_key"`
	AppSecret   string `mapstructure:"app_secret"`
	RedirectURL string `mapstructure:"redirect_url"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"` // 32-byte key for token encryption
}

// ServicesConfig holds URLs for other microservices
type ServicesConfig struct {
	CatalogURL   string `mapstructure:"catalog_url"`
	InventoryURL string `mapstructure:"inventory_url"`
	OrderURL     string `mapstructure:"order_url"`
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	v := viper.New()

	// Automatically load environment variables
	v.AutomaticEnv()
	v.SetEnvPrefix("") // No prefix, read exact variable names

	// Bind specific environment variables
	_ = v.BindEnv("app.name", "APP_NAME")
	_ = v.BindEnv("app.env", "APP_ENV")
	_ = v.BindEnv("app.port", "APP_PORT")

	_ = v.BindEnv("database.host", "DB_HOST")
	_ = v.BindEnv("database.port", "DB_PORT")
	_ = v.BindEnv("database.user", "DB_USER")
	_ = v.BindEnv("database.password", "DB_PASSWORD")
	_ = v.BindEnv("database.name", "DB_NAME")
	_ = v.BindEnv("database.ssl_mode", "DB_SSLMODE")

	_ = v.BindEnv("nats.url", "NATS_URL")

	// Redis
	_ = v.BindEnv("redis.host", "REDIS_HOST")
	_ = v.BindEnv("redis.port", "REDIS_PORT")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")

	_ = v.BindEnv("jwt.secret", "JWT_SECRET")

	_ = v.BindEnv("sentry.dsn", "SENTRY_DSN")
	_ = v.BindEnv("sentry.environment", "APP_ENV")
	_ = v.BindEnv("sentry.release", "APP_VERSION")

	// Shopee
	_ = v.BindEnv("shopee.partner_id", "SHOPEE_PARTNER_ID")
	_ = v.BindEnv("shopee.partner_key", "SHOPEE_PARTNER_KEY")
	_ = v.BindEnv("shopee.redirect_url", "SHOPEE_REDIRECT_URL")
	_ = v.BindEnv("shopee.is_sandbox", "SHOPEE_SANDBOX")

	// TikTok
	_ = v.BindEnv("tiktok.app_key", "TIKTOK_APP_KEY")
	_ = v.BindEnv("tiktok.app_secret", "TIKTOK_APP_SECRET")
	_ = v.BindEnv("tiktok.redirect_url", "TIKTOK_REDIRECT_URL")

	// Security
	_ = v.BindEnv("security.encryption_key", "MARKETPLACE_ENCRYPTION_KEY")

	// Services
	_ = v.BindEnv("services.catalog_url", "SERVICE_CATALOG_URL")
	_ = v.BindEnv("services.inventory_url", "SERVICE_INVENTORY_URL")
	_ = v.BindEnv("services.order_url", "SERVICE_ORDER_URL")

	// Set defaults
	setDefaults(v)

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func setDefaults(v *viper.Viper) {
	// App
	v.SetDefault("app.name", "service-marketplace")
	v.SetDefault("app.env", "development")
	v.SetDefault("app.port", "8009")

	// Database
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", "5432")
	v.SetDefault("database.ssl_mode", "disable")

	// NATS
	v.SetDefault("nats.url", "nats://localhost:4222")

	// Redis
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", "6379")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)

	// Shopee
	v.SetDefault("shopee.is_sandbox", true)
	v.SetDefault("shopee.redirect_url", "http://localhost:3001/marketplace/callback/shopee")

	// TikTok
	v.SetDefault("tiktok.redirect_url", "http://localhost:3001/marketplace/callback/tiktok")

	// Services
	v.SetDefault("services.catalog_url", "http://localhost:8082")
	v.SetDefault("services.inventory_url", "http://localhost:8083")
	v.SetDefault("services.order_url", "http://localhost:8005")

	// Sentry
	v.SetDefault("sentry.dsn", "")
	v.SetDefault("sentry.environment", "development")
	v.SetDefault("sentry.release", "1.0.0")
}

package config

import (
	"fmt"
	"math"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	GRPCAddress              string
	TLSCertificateFile       string
	TLSPrivateKeyFile        string
	DatabaseURL              string
	RedisURL                 string
	TrustedProxyCIDRs        []netip.Prefix
	AccessTokenSecret        string
	OrderTokenSecret         string
	AccessTokenTTL           time.Duration
	RefreshTokenTTL          time.Duration
	CartTTL                  time.Duration
	NotificationChannels     []string
	TelegramBotToken         string
	TelegramChatID           string
	NotificationPollInterval time.Duration
	DeliveryDailyLimit       int
	MinOrderTotalMinor       int64
}

func Load() (*Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}
	secret := os.Getenv("ACCESS_TOKEN_SECRET")
	if len(secret) < 32 {
		return nil, fmt.Errorf("ACCESS_TOKEN_SECRET must contain at least 32 bytes")
	}
	orderTokenSecret := os.Getenv("ORDER_TOKEN_SECRET")
	if len(orderTokenSecret) < 32 {
		return nil, fmt.Errorf("ORDER_TOKEN_SECRET must contain at least 32 bytes")
	}

	accessTTL, err := duration("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return nil, err
	}
	refreshTTL, err := duration("REFRESH_TOKEN_TTL", 30*24*time.Hour)
	if err != nil {
		return nil, err
	}
	cartTTL, err := duration("CART_TTL", 7*24*time.Hour)
	if err != nil {
		return nil, err
	}
	pollInterval, err := duration("NOTIFICATION_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return nil, err
	}
	trustedProxyCIDRs, err := prefixes("TRUSTED_PROXY_CIDRS", os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return nil, err
	}
	channels := list(os.Getenv("NOTIFICATION_CHANNELS"))
	for _, channel := range channels {
		if channel != "telegram" {
			return nil, fmt.Errorf("unsupported notification channel %q", channel)
		}
		if strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")) == "" || strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")) == "" {
			return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID are required for telegram notifications")
		}
	}
	deliveryDailyLimit, err := IntEnv("DELIVERY_DAILY_LIMIT", 20)
	if err != nil {
		return nil, err
	}
	minOrderRubles, err := IntEnv("MIN_ORDER_TOTAL_RUB", 3000)
	if err != nil {
		return nil, err
	}
	minOrderTotalMinor, err := rublesToMinor(minOrderRubles)
	if err != nil {
		return nil, err
	}
	tlsCertificateFile := strings.TrimSpace(os.Getenv("TLS_CERT_FILE"))
	tlsPrivateKeyFile := strings.TrimSpace(os.Getenv("TLS_KEY_FILE"))
	if (tlsCertificateFile == "") != (tlsPrivateKeyFile == "") {
		return nil, fmt.Errorf("TLS_CERT_FILE and TLS_KEY_FILE must be configured together")
	}

	return &Config{
		GRPCAddress:              value("GRPC_ADDRESS", ":8080"),
		TLSCertificateFile:       tlsCertificateFile,
		TLSPrivateKeyFile:        tlsPrivateKeyFile,
		DatabaseURL:              databaseURL,
		RedisURL:                 redisURL,
		TrustedProxyCIDRs:        trustedProxyCIDRs,
		AccessTokenSecret:        secret,
		OrderTokenSecret:         orderTokenSecret,
		AccessTokenTTL:           accessTTL,
		RefreshTokenTTL:          refreshTTL,
		CartTTL:                  cartTTL,
		NotificationChannels:     channels,
		TelegramBotToken:         strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramChatID:           strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		NotificationPollInterval: pollInterval,
		DeliveryDailyLimit:       deliveryDailyLimit,
		MinOrderTotalMinor:       minOrderTotalMinor,
	}, nil
}

func rublesToMinor(rubles int) (int64, error) {
	if rubles <= 0 || int64(rubles) > math.MaxInt64/100 {
		return 0, fmt.Errorf("MIN_ORDER_TOTAL_RUB must be a positive whole number of rubles")
	}
	return int64(rubles) * 100, nil
}

func prefixes(name, raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	result := make([]netip.Prefix, 0)
	seen := make(map[netip.Prefix]struct{})
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("%s contains invalid CIDR %q", name, item)
		}
		prefix = prefix.Masked()
		if _, exists := seen[prefix]; exists {
			continue
		}
		seen[prefix] = struct{}{}
		result = append(result, prefix)
	}
	return result, nil
}

func value(name, fallback string) string {
	if current := strings.TrimSpace(os.Getenv(name)); current != "" {
		return current
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}

func list(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func IntEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

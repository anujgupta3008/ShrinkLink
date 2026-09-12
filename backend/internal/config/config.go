package config

import (
	"os"
)

// Config stores all configuration parameters loaded from environment variables.
type Config struct {
	Port              string // Port is the HTTP port the backend serves on.
	DBHost            string // DBHost is the host URL for the PostgreSQL instance.
	DBPort            string // DBPort is the connection port for PostgreSQL.
	DBUser            string // DBUser is the database username.
	DBPassword        string // DBPassword is the database connection password. No default — must be set via env.
	DBName            string // DBName is the specific database name.
	DBSSLMode         string // DBSSLMode defines the connection security type.
	RedisHost         string // RedisHost is the host URL for the Redis server.
	RedisPort         string // RedisPort is the server port for Redis.
	RedisPassword     string // RedisPassword is the auth password for Redis.
	RedisDB           string // RedisDB is the database number to target in Redis.
	BaseURL           string // BaseURL is the prefix used to construct the short link.
	MigrationsPath    string // MigrationsPath is the path to the SQL migration files.
	FirebaseProjectID string // FirebaseProjectID is the Firebase project ID for auth (Level 5).
	GeoIPDBPath       string // GeoIPDBPath is the path to the MaxMind GeoLite2 mmdb file (Level 9).
}

func LoadConfig() *Config {
	return &Config{
		Port:              getEnv("PORT", "8080"),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBUser:            getEnv("DB_USER", "postgres"),
		DBPassword:        getEnv("DB_PASSWORD", ""), // No default — must be explicitly set
		DBName:            getEnv("DB_NAME", "url_shortener"),
		DBSSLMode:         getEnv("DB_SSLMODE", "disable"),
		RedisHost:         getEnv("REDIS_HOST", "localhost"),
		RedisPort:         getEnv("REDIS_PORT", "6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisDB:           getEnv("REDIS_DB", "0"),
		BaseURL:           getEnv("BASE_URL", "http://localhost:8080"),
		MigrationsPath:    getEnv("MIGRATIONS_PATH", "migrations"),
		FirebaseProjectID: getEnv("FIREBASE_PROJECT_ID", ""),
		GeoIPDBPath:       getEnv("GEOIP_DB_PATH", ""),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}


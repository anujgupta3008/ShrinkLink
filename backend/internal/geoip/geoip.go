package geoip

import (
	"log/slog"
	"net"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

// Resolver provides IP-to-country lookups using a MaxMind GeoLite2 database.
// If the database file is not found, all lookups return "Unknown" gracefully.
type Resolver struct {
	db     *geoip2.Reader
	loaded bool
	mu     sync.RWMutex
}

// NewResolver opens the GeoLite2 database file at the given path.
// If the file doesn't exist or can't be opened, the resolver still works
// but returns "Unknown" for all lookups (graceful degradation).
func NewResolver(dbPath string) *Resolver {
	r := &Resolver{}
	if dbPath == "" {
		slog.Warn("GeoIP database path not configured — all lookups will return 'Unknown'")
		return r
	}

	db, err := geoip2.Open(dbPath)
	if err != nil {
		slog.Warn("Could not open GeoIP database — all lookups will return 'Unknown'",
			"path", dbPath, "err", err)
		return r
	}

	r.db = db
	r.loaded = true
	slog.Info("GeoIP database loaded successfully", "path", dbPath)
	return r
}

// Country returns the country name for the given IP address.
// Returns "Unknown" if the IP is invalid, private, or the DB isn't loaded.
func (r *Resolver) Country(ipStr string) string {
	if !r.loaded {
		return "Unknown"
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "Unknown"
	}

	// Private / loopback IPs have no geo data
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return "Local"
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	record, err := r.db.Country(ip)
	if err != nil {
		return "Unknown"
	}

	name := record.Country.Names["en"]
	if name == "" {
		return "Unknown"
	}
	return name
}

// Close releases the underlying database resources.
func (r *Resolver) Close() {
	if r.db != nil {
		r.db.Close()
	}
}

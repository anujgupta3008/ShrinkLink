package urlvalidator

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Validator checks destination URLs for safety before allowing them to be shortened.
// It prevents SSRF attacks, self-referential loops, and malicious schemes.
type Validator struct {
	baseURL string // The application's own base URL, used to detect self-referential loops.
}

// NewValidator creates a Validator with the application's base URL for loop detection.
func NewValidator(baseURL string) *Validator {
	return &Validator{baseURL: baseURL}
}

// Validate checks a destination URL for safety.
// Returns nil if the URL is safe, or an error describing the issue.
func (v *Validator) Validate(rawURL string) error {
	// 1. Parse the URL
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// 2. Only allow http and https schemes
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http and https are allowed", parsed.Scheme)
	}

	// 3. Reject empty or missing host
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL must contain a valid hostname")
	}

	// 4. Block self-referential loops (shortening your own short URLs)
	if v.baseURL != "" {
		baseHost := extractHostname(v.baseURL)
		if strings.EqualFold(host, baseHost) {
			return fmt.Errorf("cannot shorten URLs that point back to this service")
		}
	}

	// 5. Block private/internal IP addresses (SSRF protection)
	if err := blockPrivateIPs(host); err != nil {
		return err
	}

	return nil
}

// blockPrivateIPs resolves the hostname and checks if it points to a private,
// loopback, or link-local address that should never be a redirect target.
func blockPrivateIPs(host string) error {
	// Check if the host is a raw IP address first
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("destination resolves to a blocked internal address")
		}
		return nil
	}

	// Resolve hostname to IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS resolution failure — this is likely a bad host.
		// We allow it through (the redirect will fail naturally) rather than
		// blocking, to avoid false positives on slow DNS or private DNS setups.
		return nil
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("destination resolves to a blocked internal address")
		}
	}

	return nil
}

// isBlockedIP checks if an IP is private, loopback, link-local, or otherwise
// internal and should not be used as a redirect target.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// extractHostname pulls the hostname from a URL string.
func extractHostname(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

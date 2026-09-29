package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeaders adds essential security headers to every HTTP response.
// These headers protect against common web vulnerabilities:
//
//   - X-Content-Type-Options: prevents MIME-type sniffing attacks
//   - X-Frame-Options: prevents clickjacking by blocking iframe embedding
//   - Referrer-Policy: limits referrer leakage to external sites
//   - X-XSS-Protection: legacy XSS filter (still respected by older browsers)
//   - Content-Security-Policy: restricts resource loading origins
//   - Strict-Transport-Security: enforces HTTPS (only effective behind TLS)
//   - Permissions-Policy: disables unnecessary browser features
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()

		// Prevent MIME-type sniffing — browser must respect declared Content-Type
		h.Set("X-Content-Type-Options", "nosniff")

		// Prevent this page from being embedded in iframes (clickjacking defense)
		h.Set("X-Frame-Options", "DENY")

		// Only send the origin as referrer to external sites (not the full path)
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Legacy XSS filter — harmless on modern browsers, helps older ones
		h.Set("X-XSS-Protection", "1; mode=block")

		// Content Security Policy — restrict where resources can be loaded from.
		// This is a baseline policy; adjust if you serve inline scripts or fonts from CDNs.
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://www.gstatic.com https://apis.google.com; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data: https:; connect-src 'self' https://*.googleapis.com https://*.firebaseio.com")

		// HSTS — tell browsers to only use HTTPS for 1 year (including subdomains).
		// Only effective when served over TLS; harmless over plain HTTP.
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Disable unnecessary browser features
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		c.Next()
	}
}

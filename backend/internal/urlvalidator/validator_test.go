package urlvalidator

import (
	"testing"
)

func TestValidate_SafeURLs(t *testing.T) {
	v := NewValidator("http://localhost:8080")

	safeURLs := []string{
		"https://www.google.com",
		"https://github.com/user/repo",
		"http://example.com/path?q=1#frag",
		"https://subdomain.example.co.uk/deep/path",
	}

	for _, u := range safeURLs {
		if err := v.Validate(u); err != nil {
			t.Errorf("expected safe URL %q to pass, got error: %v", u, err)
		}
	}
}

func TestValidate_BlockedSchemes(t *testing.T) {
	v := NewValidator("")

	blockedURLs := []string{
		"ftp://files.example.com/data.csv",
		"javascript:alert(1)",
		"data:text/html,<h1>hi</h1>",
		"file:///etc/passwd",
	}

	for _, u := range blockedURLs {
		if err := v.Validate(u); err == nil {
			t.Errorf("expected blocked scheme URL %q to fail, but it passed", u)
		}
	}
}

func TestValidate_SelfReferentialLoop(t *testing.T) {
	v := NewValidator("http://localhost:8080")

	loopURLs := []string{
		"http://localhost:8080/abc123",
		"http://localhost:8080/some/path",
		"https://localhost:8080/test",
	}

	for _, u := range loopURLs {
		if err := v.Validate(u); err == nil {
			t.Errorf("expected self-referential URL %q to fail, but it passed", u)
		}
	}
}

func TestValidate_PrivateIPs(t *testing.T) {
	v := NewValidator("")

	privateURLs := []string{
		"http://127.0.0.1/admin",
		"http://127.0.0.1:8080/secret",
		"http://10.0.0.1/internal",
		"http://172.16.0.1/private",
		"http://192.168.1.1/router",
		"http://[::1]/ipv6-loopback",
		"http://0.0.0.0/",
	}

	for _, u := range privateURLs {
		if err := v.Validate(u); err == nil {
			t.Errorf("expected private IP URL %q to fail, but it passed", u)
		}
	}
}

func TestValidate_EmptyHost(t *testing.T) {
	v := NewValidator("")

	if err := v.Validate("http:///no-host"); err == nil {
		t.Error("expected empty host URL to fail, but it passed")
	}
}

func TestValidate_InvalidURL(t *testing.T) {
	v := NewValidator("")

	invalidURLs := []string{
		"not-a-url",
		"",
		"://missing-scheme",
	}

	for _, u := range invalidURLs {
		if err := v.Validate(u); err == nil {
			t.Errorf("expected invalid URL %q to fail, but it passed", u)
		}
	}
}

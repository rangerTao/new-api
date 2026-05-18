package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

// IsPathAllowed: empty prefixes denies everything (fail-safe), non-empty
// matches by prefix.
func TestPassthroughConfig_IsPathAllowed(t *testing.T) {
	cases := []struct {
		name     string
		prefixes []string
		path     string
		want     bool
	}{
		{"empty prefixes denies all", nil, "/api/foo", false},
		{"empty path denied", []string{"/api/"}, "", false},
		{"prefix match", []string{"/api/layout/"}, "/api/layout/detect_file", true},
		{"non-prefix path denied", []string{"/api/layout/"}, "/admin/secret", false},
		{"sibling path denied", []string{"/api/layout/"}, "/api/layout-other/x", false},
		{"multiple prefixes — first match", []string{"/health", "/api/layout/"}, "/health", true},
		{"empty-string entries skipped", []string{"", "/api/x/"}, "/api/x/y", true},
		{"empty-string entries don't match all", []string{""}, "/anything", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := dto.PassthroughConfig{AllowedPathPrefixes: tc.prefixes}
			if got := cfg.IsPathAllowed(tc.path); got != tc.want {
				t.Fatalf("IsPathAllowed(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// IsMethodAllowed: empty list = all methods OK; case-insensitive match.
func TestPassthroughConfig_IsMethodAllowed(t *testing.T) {
	cases := []struct {
		name    string
		methods []string
		method  string
		want    bool
	}{
		{"empty allows everything", nil, "POST", true},
		{"empty allows GET", nil, "GET", true},
		{"explicit POST allowed", []string{"POST"}, "POST", true},
		{"explicit POST blocks GET", []string{"POST"}, "GET", false},
		{"case-insensitive", []string{"post"}, "POST", true},
		{"multiple allowed", []string{"GET", "HEAD"}, "HEAD", true},
		{"multiple denies others", []string{"GET", "HEAD"}, "DELETE", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := dto.PassthroughConfig{AllowedMethods: tc.methods}
			if got := cfg.IsMethodAllowed(tc.method); got != tc.want {
				t.Fatalf("IsMethodAllowed(%q) = %v, want %v", tc.method, got, tc.want)
			}
		})
	}
}

// normalizePath: empty -> "/", missing leading "/" -> prepended.
func TestNormalizePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/api/x", "/api/x"},
		{"api/x", "/api/x"}, // gin sometimes yields catch-all without leading slash
	}
	for _, tc := range cases {
		if got := normalizePath(tc.in); got != tc.want {
			t.Fatalf("normalizePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// injectPassthroughAuth: covers each Type branch and required-field validation.
func TestInjectPassthroughAuth(t *testing.T) {
	t.Run("nil auth no-op", func(t *testing.T) {
		h := http.Header{}
		if err := injectPassthroughAuth(h, nil, "any-key"); err != nil {
			t.Fatalf("nil auth should be no-op, got %v", err)
		}
		if got := h.Get("Authorization"); got != "" {
			t.Fatalf("nil auth must not set Authorization, got %q", got)
		}
	})
	t.Run("none type", func(t *testing.T) {
		h := http.Header{}
		err := injectPassthroughAuth(h, &dto.PassthroughAuth{Type: "none"}, "any")
		if err != nil || h.Get("Authorization") != "" {
			t.Fatalf("none type must skip; err=%v Authorization=%q", err, h.Get("Authorization"))
		}
	})
	t.Run("bearer", func(t *testing.T) {
		h := http.Header{}
		err := injectPassthroughAuth(h, &dto.PassthroughAuth{Type: dto.PassthroughAuthBearer}, "tok-123")
		if err != nil {
			t.Fatalf("bearer err: %v", err)
		}
		if h.Get("Authorization") != "Bearer tok-123" {
			t.Fatalf("bearer header wrong: %q", h.Get("Authorization"))
		}
	})
	t.Run("bearer rejects empty key", func(t *testing.T) {
		h := http.Header{}
		err := injectPassthroughAuth(h, &dto.PassthroughAuth{Type: dto.PassthroughAuthBearer}, "")
		if err == nil {
			t.Fatalf("bearer with empty key should error")
		}
	})
	t.Run("custom header", func(t *testing.T) {
		h := http.Header{}
		err := injectPassthroughAuth(h,
			&dto.PassthroughAuth{Type: dto.PassthroughAuthHeader, HeaderName: "X-Api-Key"},
			"k1")
		if err != nil {
			t.Fatalf("header err: %v", err)
		}
		if h.Get("X-Api-Key") != "k1" {
			t.Fatalf("custom header wrong: %q", h.Get("X-Api-Key"))
		}
		if h.Get("Authorization") != "" {
			t.Fatalf("Authorization must not be set in header mode")
		}
	})
	t.Run("header rejects empty header name", func(t *testing.T) {
		h := http.Header{}
		err := injectPassthroughAuth(h,
			&dto.PassthroughAuth{Type: dto.PassthroughAuthHeader, HeaderName: ""},
			"k1")
		if err == nil {
			t.Fatalf("header with empty HeaderName should error")
		}
	})
	t.Run("basic encodes channel.Key as user:pass", func(t *testing.T) {
		h := http.Header{}
		err := injectPassthroughAuth(h, &dto.PassthroughAuth{Type: dto.PassthroughAuthBasic}, "alice:s3cret")
		if err != nil {
			t.Fatalf("basic err: %v", err)
		}
		// base64("alice:s3cret") = YWxpY2U6czNjcmV0
		if h.Get("Authorization") != "Basic YWxpY2U6czNjcmV0" {
			t.Fatalf("basic header wrong: %q", h.Get("Authorization"))
		}
	})
	t.Run("unknown type errors", func(t *testing.T) {
		h := http.Header{}
		err := injectPassthroughAuth(h, &dto.PassthroughAuth{Type: "weird"}, "k")
		if err == nil {
			t.Fatalf("unknown type should error")
		}
	})
}

// copyClientHeaders: hop-by-hop headers stripped, downstream-only stripped,
// other headers forwarded verbatim including duplicates.
func TestCopyClientHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Type", "application/json")
	src.Set("X-Custom", "abc")
	src.Add("X-Multi", "v1")
	src.Add("X-Multi", "v2")
	src.Set("Authorization", "Bearer leaked-token") // must NOT propagate
	src.Set("Cookie", "session=foo")                 // must NOT propagate
	src.Set("Connection", "keep-alive")              // hop-by-hop, must NOT propagate
	src.Set("X-Forwarded-For", "1.2.3.4")            // downstream-only, must NOT propagate

	dst := http.Header{}
	copyClientHeaders(src, dst)

	if got := dst.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type missing: %q", got)
	}
	if got := dst.Get("X-Custom"); got != "abc" {
		t.Fatalf("X-Custom missing: %q", got)
	}
	if got := dst["X-Multi"]; len(got) != 2 || got[0] != "v1" || got[1] != "v2" {
		t.Fatalf("X-Multi values wrong: %v", got)
	}
	for _, banned := range []string{"Authorization", "Cookie", "Connection", "X-Forwarded-For"} {
		if got := dst.Get(banned); got != "" {
			t.Fatalf("banned header %q leaked through: %q", banned, got)
		}
	}
}

// isStreamingResponse: chunked transfer, SSE content-type, unknown length all
// trigger streaming mode; finite content-length stays non-streaming.
func TestIsStreamingResponse(t *testing.T) {
	cases := []struct {
		name string
		resp *http.Response
		want bool
	}{
		{
			name: "chunked transfer",
			resp: &http.Response{TransferEncoding: []string{"chunked"}, ContentLength: -1, Header: http.Header{}},
			want: true,
		},
		{
			name: "SSE content-type",
			resp: &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}}, ContentLength: 100},
			want: true,
		},
		{
			name: "unknown length (no Content-Length)",
			resp: &http.Response{Header: http.Header{}, ContentLength: -1},
			want: true,
		},
		{
			name: "finite JSON response",
			resp: &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, ContentLength: 256},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStreamingResponse(tc.resp); got != tc.want {
				t.Fatalf("isStreamingResponse = %v, want %v", got, tc.want)
			}
		})
	}
}

// forwardUpstreamResponse: integration-ish — drain a synthesized resp into a
// gin-style ResponseRecorder and assert headers + body propagate.
func TestForwardUpstreamResponse_NonStreaming(t *testing.T) {
	body := strings.NewReader(`{"ok":true}`)
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"application/json"}, "X-Volc-Logid": []string{"abc"}},
		Body:          io.NopCloser(body),
		ContentLength: int64(body.Len()),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	forwardUpstreamResponse(c, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("status mismatch: %d", rec.Code)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Fatalf("body mismatch: %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type lost: %q", got)
	}
	if got := rec.Header().Get("X-Volc-Logid"); got != "abc" {
		t.Fatalf("X-Volc-Logid lost: %q", got)
	}
}

// resolveResolvedPassthroughDefault: nil settings yields an empty (deny-all) config.
func TestResolvedPassthrough_Defaults(t *testing.T) {
	var s *dto.ChannelOtherSettings
	cfg := s.ResolvedPassthrough()
	if cfg.IsPathAllowed("/anything") {
		t.Fatalf("nil settings must deny all paths")
	}
	if !cfg.IsMethodAllowed("POST") {
		t.Fatalf("nil settings must default-allow methods (only path is fail-safe)")
	}

	s = &dto.ChannelOtherSettings{}
	cfg = s.ResolvedPassthrough()
	if cfg.IsPathAllowed("/anything") {
		t.Fatalf("empty Passthrough must deny all paths")
	}
}


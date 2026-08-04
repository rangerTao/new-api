package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

func TestWebBasePathRouterCanRegisterBeforeRelayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	webRouter := engine.Group("/web")
	webRouter.Use(StripWebBasePath("/web"))

	SetApiRouter(engine)
	SetDashboardRouter(engine)
	SetApiRouter(webRouter)
	SetDashboardRouter(webRouter)
	SetRelayRouter(engine)
	SetVideoRouter(engine)
	SetRelayRouter(webRouter)
	SetVideoRouter(webRouter)

	assertRouteRegistered(t, engine, http.MethodPost, "/web/v1/responses")
	assertRouteRegistered(t, engine, http.MethodPost, "/web/v1/chat/completions")
	assertRouteRegistered(t, engine, http.MethodGet, "/web/v1/models")
}

func TestStripWebBasePathRestoresRelayModePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		method   string
		path     string
		stripped string
		mode     int
	}{
		{
			name:     "audio speech",
			method:   http.MethodPost,
			path:     "/web/v1/audio/speech?x=1",
			stripped: "/v1/audio/speech",
			mode:     relayconstant.RelayModeAudioSpeech,
		},
		{
			name:     "responses",
			method:   http.MethodPost,
			path:     "/web/v1/responses",
			stripped: "/v1/responses",
			mode:     relayconstant.RelayModeResponses,
		},
		{
			name:     "chat completions",
			method:   http.MethodPost,
			path:     "/web/v1/chat/completions",
			stripped: "/v1/chat/completions",
			mode:     relayconstant.RelayModeChatCompletions,
		},
		{
			name:     "images",
			method:   http.MethodPost,
			path:     "/web/v1/images/generations",
			stripped: "/v1/images/generations",
			mode:     relayconstant.RelayModeImagesGenerations,
		},
		{
			name:     "embeddings",
			method:   http.MethodPost,
			path:     "/web/v1/embeddings",
			stripped: "/v1/embeddings",
			mode:     relayconstant.RelayModeEmbeddings,
		},
		{
			name:     "gemini model path",
			method:   http.MethodPost,
			path:     "/web/v1beta/models/gemini-2.5-flash:embedContent",
			stripped: "/v1beta/models/gemini-2.5-flash:embedContent",
			mode:     relayconstant.RelayModeGemini,
		},
		{
			name:     "midjourney",
			method:   http.MethodPost,
			path:     "/web/mj/submit/action",
			stripped: "/mj/submit/action",
			mode:     relayconstant.RelayModeMidjourneyAction,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := gin.New()
			webRouter := engine.Group("/web")
			webRouter.Use(StripWebBasePath("/web"))
			webRouter.Any("/*path", func(c *gin.Context) {
				if c.Request.URL.Path != tt.stripped {
					t.Fatalf("stripped path = %q, want %q", c.Request.URL.Path, tt.stripped)
				}
				if mode := relayconstant.Path2RelayMode(c.Request.URL.Path); mode != tt.mode {
					t.Fatalf("relay mode = %d, want %d", mode, tt.mode)
				}
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			resp := httptest.NewRecorder()
			engine.ServeHTTP(resp, req)

			if resp.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", resp.Code, http.StatusNoContent)
			}
		})
	}
}

func TestStripWebBasePathRestoresRequestURIForUpstreamPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	webRouter := engine.Group("/web")
	webRouter.Use(StripWebBasePath("/web"))
	webRouter.POST("/v1/custom/:model/*path", func(c *gin.Context) {
		if c.Request.URL.Path != "/v1/custom/demo/health" {
			t.Fatalf("stripped path = %q, want /v1/custom/demo/health", c.Request.URL.Path)
		}
		if c.Request.RequestURI != "/v1/custom/demo/health?trace=1" {
			t.Fatalf("request URI = %q, want /v1/custom/demo/health?trace=1", c.Request.RequestURI)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/web/v1/custom/demo/health?trace=1", nil)
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNoContent)
	}
}

func assertRouteRegistered(t *testing.T, engine *gin.Engine, method string, path string) {
	t.Helper()
	for _, route := range engine.Routes() {
		if route.Method == method && route.Path == path {
			return
		}
	}
	t.Fatalf("route %s %s is not registered", method, path)
}

func TestNormalizeWebBasePath(t *testing.T) {
	tests := map[string]string{
		"":       "",
		"/":      "",
		"web":    "/web",
		"/web":   "/web",
		"/web/":  "/web",
		" /web/": "/web",
	}

	for input, expected := range tests {
		if actual := NormalizeWebBasePath(input); actual != expected {
			t.Fatalf("NormalizeWebBasePath(%q) = %q, want %q", input, actual, expected)
		}
	}
}

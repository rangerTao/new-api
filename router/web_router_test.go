package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWebBasePathRouterCanRegisterBeforeRelayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	assets := ThemeAssets{
		DefaultIndexPage: []byte("<!doctype html><div id=\"root\"></div>"),
		ClassicIndexPage: []byte("<!doctype html><div id=\"root\"></div>"),
	}

	SetWebBasePathRouter(engine, assets, "/web")
	SetApiRouter(engine)
	SetDashboardRouter(engine)
	SetRelayRouter(engine)
	SetVideoRouter(engine)
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

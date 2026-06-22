package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWebBasePathRouterCanRegisterBeforeRelayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	webRouter := engine.Group("/web")

	SetApiRouter(engine)
	SetDashboardRouter(engine)
	SetApiRouter(webRouter)
	SetDashboardRouter(webRouter)
	SetRelayRouter(engine)
	SetVideoRouter(engine)
	SetRelayRouter(webRouter)
	SetVideoRouter(webRouter)
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

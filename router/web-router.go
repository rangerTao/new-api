package router

import (
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

func NormalizeWebBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return strings.TrimRight(value, "/")
}

func SetWebRouter(router *gin.Engine, assets ThemeAssets, basePath string) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := common.NewThemeAwareFS(defaultFS, classicFS)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", themeFS))
	basePath = NormalizeWebBasePath(basePath)
	if basePath != "" {
		router.Use(static.Serve(basePath, themeFS))
	}
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if isAPILikePath(c.Request.URL.Path, basePath) {
			controller.RelayNotFound(c)
			return
		}
		serveIndexPage(c, assets)
	})
}

func isAPILikePath(path string, basePath string) bool {
	basePath = NormalizeWebBasePath(basePath)
	if basePath != "" && strings.HasPrefix(path, basePath+"/") {
		path = strings.TrimPrefix(path, basePath)
	}
	return strings.HasPrefix(path, "/v1") ||
		strings.HasPrefix(path, "/api") ||
		strings.HasPrefix(path, "/assets")
}

func serveIndexPage(c *gin.Context, assets ThemeAssets) {
	c.Header("Cache-Control", "no-cache")
	if common.GetTheme() == "classic" {
		c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ClassicIndexPage)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", assets.DefaultIndexPage)
}

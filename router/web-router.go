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

func StripWebBasePath(basePath string) gin.HandlerFunc {
	basePath = NormalizeWebBasePath(basePath)
	return func(c *gin.Context) {
		if basePath == "" || !strings.HasPrefix(c.Request.URL.Path, basePath) {
			c.Next()
			return
		}

		originalPath := c.Request.URL.Path
		originalRawPath := c.Request.URL.RawPath
		originalRequestURI := c.Request.RequestURI

		c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, basePath)
		if c.Request.URL.Path == "" {
			c.Request.URL.Path = "/"
		}
		if c.Request.URL.RawPath != "" && strings.HasPrefix(c.Request.URL.RawPath, basePath) {
			c.Request.URL.RawPath = strings.TrimPrefix(c.Request.URL.RawPath, basePath)
			if c.Request.URL.RawPath == "" {
				c.Request.URL.RawPath = "/"
			}
		}
		c.Request.RequestURI = c.Request.URL.RequestURI()

		c.Next()

		c.Request.URL.Path = originalPath
		c.Request.URL.RawPath = originalRawPath
		c.Request.RequestURI = originalRequestURI
	}
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

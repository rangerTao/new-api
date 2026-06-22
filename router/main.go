package router

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets ThemeAssets) {
	webBasePath := NormalizeWebBasePath(os.Getenv("WEB_BASE_PATH"))
	var webRouter *gin.RouterGroup
	if webBasePath != "" {
		webRouter = router.Group(webBasePath)
	}
	SetApiRouter(router)
	SetDashboardRouter(router)
	if webRouter != nil {
		SetApiRouter(webRouter)
		SetDashboardRouter(webRouter)
	}
	SetRelayRouter(router)
	SetVideoRouter(router)
	if webRouter != nil {
		SetRelayRouter(webRouter)
		SetVideoRouter(webRouter)
	}
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets, webBasePath)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(c *gin.Context) {
			c.Set(middleware.RouteTagKey, "web")
			c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, c.Request.RequestURI))
		})
	}
}

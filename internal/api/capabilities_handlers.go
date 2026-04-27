package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerCapabilityRoutes(router *gin.Engine, deps Dependencies) {
	router.GET("/api/capabilities", func(c *gin.Context) {
		c.JSON(http.StatusOK, deps.Capabilities)
	})
}

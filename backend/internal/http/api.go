package http

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

type APIRoutes struct {
	Auth           *auth.Handler
	Authentication gin.HandlerFunc
}

func registerAPIV1Routes(router *gin.Engine, routes APIRoutes) {
	api := router.Group("/api/v1")
	if routes.Auth == nil {
		return
	}

	authRoutes := api.Group("/auth")
	authRoutes.POST("/register", routes.Auth.Register)
	authRoutes.POST("/login", routes.Auth.Login)
	authRoutes.POST("/logout", routes.Auth.Logout)

	if routes.Authentication != nil {
		users := api.Group("/users")
		users.Use(routes.Authentication)
		users.GET("/me", routes.Auth.CurrentUser)
	}
}

package http

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/gin-gonic/gin"
)

type APIRoutes struct {
	Auth           *auth.Handler
	Posts          *post.Handler
	Authentication gin.HandlerFunc
}

func registerAPIV1Routes(router *gin.Engine, routes APIRoutes) {
	api := router.Group("/api/v1")
	if routes.Auth != nil {
		authRoutes := api.Group("/auth")
		authRoutes.POST("/register", routes.Auth.Register)
		authRoutes.POST("/login", routes.Auth.Login)
		authRoutes.POST("/logout", routes.Auth.Logout)
	}

	if routes.Authentication == nil {
		return
	}
	protected := api.Group("")
	protected.Use(routes.Authentication)
	if routes.Auth != nil {
		protected.GET("/users/me", routes.Auth.CurrentUser)
	}
	if routes.Posts != nil {
		protected.POST("/posts", routes.Posts.Create)
		protected.GET("/posts", routes.Posts.List)
		protected.GET("/posts/:postId", routes.Posts.Detail)
	}
}

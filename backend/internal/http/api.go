package http

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/auth"
	"github.com/Ray-ymq/GoPulse/backend/internal/comment"
	"github.com/Ray-ymq/GoPulse/backend/internal/eventquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin"
	"github.com/Ray-ymq/GoPulse/backend/internal/like"
	"github.com/Ray-ymq/GoPulse/backend/internal/logquery"
	"github.com/Ray-ymq/GoPulse/backend/internal/notification"
	"github.com/Ray-ymq/GoPulse/backend/internal/post"
	"github.com/Ray-ymq/GoPulse/backend/internal/search"
	"github.com/gin-gonic/gin"
)

type APIRoutes struct {
	Auth            *auth.Handler
	Posts           *post.Handler
	Comments        *comment.Handler
	Likes           *like.Handler
	Logs            *logquery.Handler
	Events          *eventquery.Handler
	Notifications   *notification.Handler
	Search          *search.Handler
	Authentication  gin.HandlerFunc
	Authorization   gin.HandlerFunc
	ExporterPlugins *exporterplugin.Handler
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
	if routes.Comments != nil {
		protected.POST("/posts/:postId/comments", routes.Comments.Create)
		protected.GET("/posts/:postId/comments", routes.Comments.List)
	}
	if routes.Likes != nil {
		protected.PUT("/posts/:postId/like", routes.Likes.Like)
		protected.DELETE("/posts/:postId/like", routes.Likes.Unlike)
	}
	if routes.Search != nil {
		protected.GET("/search/posts", routes.Search.Posts)
	}
	if routes.Notifications != nil {
		protected.GET("/notifications", routes.Notifications.List)
		protected.PATCH("/notifications/:notificationId/read", routes.Notifications.MarkRead)
	}
	if (routes.Logs != nil || routes.Events != nil) && routes.Authorization != nil {
		observability := protected.Group("/observability")
		observability.Use(routes.Authorization)
		if routes.Logs != nil {
			observability.GET("/logs", routes.Logs.List)
		}
		if routes.Events != nil {
			observability.GET("/events", routes.Events.List)
		}
	}
	if routes.ExporterPlugins != nil && routes.Authorization != nil {
		plugins := protected.Group("/exporter-plugins")
		plugins.Use(routes.Authorization)
		plugins.GET("", routes.ExporterPlugins.List)
		plugins.GET("/:pluginId", routes.ExporterPlugins.Get)
		plugins.POST("/install", routes.ExporterPlugins.Install)
		plugins.POST("/:pluginId/start", routes.ExporterPlugins.Start)
		plugins.POST("/:pluginId/stop", routes.ExporterPlugins.Stop)
		plugins.POST("/:pluginId/update", routes.ExporterPlugins.Update)
	}
}

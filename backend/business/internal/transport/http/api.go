package http

import (
	authhttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/auth"
	bookmarkhttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/bookmark"
	commenthttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/comment"
	likehttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/like"
	notificationhttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/notification"
	posthttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/post"
	searchhttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/search"
	userhttp "github.com/Ray-ymq/GoPulse/backend/internal/transport/http/handlers/user"

	"github.com/gin-gonic/gin"
)

type APIRoutes struct {
	Bookmarks            *bookmarkhttp.Handler
	Users                *userhttp.Handler
	Auth                 *authhttp.Handler
	Posts                *posthttp.Handler
	Comments             *commenthttp.Handler
	Likes                *likehttp.Handler
	Notifications        *notificationhttp.Handler
	Search               *searchhttp.Handler
	Authentication       gin.HandlerFunc
	PublicRateLimit      gin.HandlerFunc
	ProtectedRateLimit   gin.HandlerFunc
	PublicResourceBudget gin.HandlerFunc
	ResourceBudget       gin.HandlerFunc
}

func registerAPIV1Routes(router *gin.Engine, routes APIRoutes) {
	api := router.Group("/api/v1")
	if routes.Auth != nil {
		authRoutes := api.Group("/auth")
		if routes.PublicRateLimit != nil {
			authRoutes.Use(routes.PublicRateLimit)
		}
		if routes.PublicResourceBudget != nil {
			authRoutes.Use(routes.PublicResourceBudget)
		}
		authRoutes.POST("/register", routes.Auth.Register)
		authRoutes.POST("/login", routes.Auth.Login)
		authRoutes.POST("/logout", routes.Auth.Logout)
	}

	if routes.Authentication == nil {
		return
	}

	protected := api.Group("")
	protected.Use(routes.Authentication)
	if routes.ProtectedRateLimit != nil {
		protected.Use(routes.ProtectedRateLimit)
	}
	if routes.ResourceBudget != nil {
		protected.Use(routes.ResourceBudget)
	}
	if routes.Auth != nil {
		protected.GET("/users/me", routes.Auth.CurrentUser)
	}
	if routes.Users != nil {
		protected.PATCH("/users/me/profile", routes.Users.Update)
		if routes.Users.HasFollows() {
			protected.PUT("/users/:userId/follow", routes.Users.Follow)
			protected.DELETE("/users/:userId/follow", routes.Users.Follow)
			protected.GET("/users/me/following", routes.Users.Relations)
			protected.GET("/users/me/followers", routes.Users.Relations)
			protected.GET("/posts/following", routes.Users.Following)
		}
		protected.GET("/users/:username", routes.Users.Get)
		protected.GET("/users/:username/posts", routes.Users.Posts)
		protected.GET("/search/users", routes.Users.Search)
	}
	if routes.Posts != nil {
		protected.GET("/post-commands/context", routes.Posts.CommandContext)
		protected.GET("/post-commands/result", routes.Posts.CommandResult)
		protected.POST("/posts", routes.Posts.Create)
		protected.GET("/posts", routes.Posts.List)
		protected.GET("/bookmarks", routes.Posts.Bookmarks)
		protected.GET("/posts/:postId", routes.Posts.Detail)
		protected.PATCH("/posts/:postId", routes.Posts.Update)
		protected.DELETE("/posts/:postId", routes.Posts.Delete)
	}
	if routes.Comments != nil {
		protected.POST("/posts/:postId/comments", routes.Comments.Create)
		protected.GET("/posts/:postId/comments", routes.Comments.List)
	}
	if routes.Bookmarks != nil {
		protected.PUT("/posts/:postId/bookmark", routes.Bookmarks.Bookmark)
		protected.DELETE("/posts/:postId/bookmark", routes.Bookmarks.Unbookmark)
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
}

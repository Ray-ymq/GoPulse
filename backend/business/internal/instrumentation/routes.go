package instrumentation

// BackendRoutes freezes the complete production router, including health/readiness.
func BackendRoutes() []Route {
	return []Route{

		{Method: "GET", Template: "/health"},
		{Method: "GET", Template: "/startup"},
		{Method: "GET", Template: "/live"},
		{Method: "GET", Template: "/ready"},
		{Method: "POST", Template: "/api/v1/auth/register"},
		{Method: "POST", Template: "/api/v1/auth/login"},
		{Method: "POST", Template: "/api/v1/auth/logout"},
		{Method: "GET", Template: "/api/v1/users/me"},
		{Method: "PATCH", Template: "/api/v1/users/me/profile"},
		{Method: "PUT", Template: "/api/v1/users/:userId/follow"},
		{Method: "DELETE", Template: "/api/v1/users/:userId/follow"},
		{Method: "GET", Template: "/api/v1/users/me/following"},
		{Method: "GET", Template: "/api/v1/users/me/followers"},
		{Method: "GET", Template: "/api/v1/posts/following"},
		{Method: "GET", Template: "/api/v1/users/:username"},
		{Method: "GET", Template: "/api/v1/users/:username/posts"},
		{Method: "GET", Template: "/api/v1/search/users"},
		{Method: "POST", Template: "/api/v1/posts"},
		{Method: "GET", Template: "/api/v1/posts"},
		{Method: "GET", Template: "/api/v1/bookmarks"},
		{Method: "GET", Template: "/api/v1/posts/:postId"},
		{Method: "PATCH", Template: "/api/v1/posts/:postId"},
		{Method: "DELETE", Template: "/api/v1/posts/:postId"},
		{Method: "POST", Template: "/api/v1/posts/:postId/comments"},
		{Method: "GET", Template: "/api/v1/posts/:postId/comments"},
		{Method: "PUT", Template: "/api/v1/posts/:postId/bookmark"},
		{Method: "DELETE", Template: "/api/v1/posts/:postId/bookmark"},
		{Method: "PUT", Template: "/api/v1/posts/:postId/like"},
		{Method: "DELETE", Template: "/api/v1/posts/:postId/like"},
		{Method: "GET", Template: "/api/v1/search/posts"},
		{Method: "GET", Template: "/api/v1/notifications"},
		{Method: "PATCH", Template: "/api/v1/notifications/:notificationId/read"},
	}
}

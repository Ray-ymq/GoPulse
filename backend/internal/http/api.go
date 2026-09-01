package http

import "github.com/gin-gonic/gin"

// registerAPIV1Routes establishes the Phase 1 API boundary. Business modules
// register their handlers on this group in later implementation batches.
func registerAPIV1Routes(router *gin.Engine) {
	router.Group("/api/v1")
}

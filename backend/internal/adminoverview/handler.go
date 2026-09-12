package adminoverview

import (
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/Ray-ymq/GoPulse/backend/internal/http/response"
	"github.com/gin-gonic/gin"
)

func (s *Service) Handle(c *gin.Context) {
	q := c.Request.URL.Query()
	if len(q) != 1 || len(q["range"]) != 1 || q.Get("range") != "15m" {
		response.Error(c, apperror.New(apperror.CodeValidationFailed, "invalid overview request"))
		return
	}
	response.Data(c, 200, s.Overview(c.Request.Context()))
}

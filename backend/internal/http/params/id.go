package params

import (
	"strconv"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/gin-gonic/gin"
)

// PositiveID parses an unsigned, non-zero path identifier.
func PositiveID(c *gin.Context, name string) (uint64, error) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		return 0, apperror.New(apperror.CodeValidationFailed, name+" must be a positive integer")
	}
	return value, nil
}

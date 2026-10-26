package params

import (
	"net/http/httptest"
	"testing"

	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"github.com/gin-gonic/gin"
)

func TestPositiveID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		value string
		want  uint64
		valid bool
	}{
		{value: "42", want: 42, valid: true},
		{value: "0", valid: false},
		{value: "-1", valid: false},
		{value: "invalid", valid: false},
		{value: "18446744073709551616", valid: false},
	} {
		t.Run(test.value, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Params = gin.Params{{Key: "postId", Value: test.value}}
			id, err := PositiveID(context, "postId")
			if test.valid {
				if err != nil || id != test.want {
					t.Fatalf("PositiveID() = %d, %v; want %d, nil", id, err, test.want)
				}
				return
			}
			appError, ok := apperror.As(err)
			if !ok || appError.Code != apperror.CodeValidationFailed {
				t.Fatalf("PositiveID() error = %#v, want validation_failed", err)
			}
		})
	}
}

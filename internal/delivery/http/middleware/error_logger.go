package middleware

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"
)

const errorCodeContextKey = "http_error_code"

func ErrorLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		status := c.Writer.Status()
		if status < 400 {
			return
		}

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		code, _ := c.Get(errorCodeContextKey)
		details := make([]string, 0, len(c.Errors))
		for _, requestErr := range c.Errors {
			details = append(details, requestErr.Error())
		}
		log.Printf(
			"HTTP error status=%d method=%s path=%s code=%v details=%q",
			status, c.Request.Method, path, code, strings.Join(details, "; "),
		)
	}
}

package middleware

import (
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		allowedOrigins := []string{
			"*",
		}

		origin := ctx.Request.Header.Get("Origin")
		var isAllowed bool
		for _, allowedOrigin := range allowedOrigins {
			if origin == allowedOrigin {
				isAllowed = true
				break
			}
		}

		if isAllowed {
			ctx.Header("Access-Control-Allow-Origin", origin) // Dynamically set allowed origin
			ctx.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			ctx.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			ctx.Header("Access-Control-Allow-Credentials", "true") // Correct header for credentials
		}

		if ctx.Request.Method == "OPTIONS" {
			ctx.AbortWithStatus(204)
			return
		}

		ctx.Next()
	}
}

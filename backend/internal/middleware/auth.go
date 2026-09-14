package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"url-shortener/internal/model"
	"url-shortener/internal/redis"
	"url-shortener/internal/service"
)

// FirebaseAuth returns a Gin middleware that verifies Firebase ID tokens.
//
// When `required` is true the request is rejected with 401 if no valid token
// is present. When false the request proceeds as anonymous — downstream
// handlers check c.Get("userID") to branch on logged-in vs anonymous.
func FirebaseAuth(authClient *firebase.Client, userService *service.UserService, rdb *redis.Client, required bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")

		// No token supplied
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			if required {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid authorization header"})
				c.Abort()
				return
			}
			// Optional auth — continue as anonymous
			c.Next()
			return
		}

		idToken := strings.TrimPrefix(header, "Bearer ")

		// Verify the Firebase ID token
		token, err := authClient.VerifyIDToken(c.Request.Context(), idToken)
		if err != nil {
			slog.Warn("Invalid Firebase ID token", "err", err)
			if required {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
				c.Abort()
				return
			}
			// Optional auth — invalid token treated as anonymous
			c.Next()
			return
		}

		email, _ := token.Claims["email"].(string)

		var user *model.User
		syncKey := "user_sync:" + token.UID
		
		// 1. Try to get cached user profile
		if val, err := rdb.Get(c.Request.Context(), syncKey).Result(); err == nil {
			// Cache hit: unmarshal the minimal cached user
			var cachedUser model.User
			if jsonErr := json.Unmarshal([]byte(val), &cachedUser); jsonErr == nil {
				user = &cachedUser
			}
		}

		// 2. Cache miss or parse error: hit DB and cache
		if user == nil {
			var err error
			user, err = userService.GetOrCreate(c.Request.Context(), token.UID, email)
			if err != nil {
				slog.Error("Failed to upsert user from token", "uid", token.UID, "err", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
				c.Abort()
				return
			}
			
			// Fire-and-forget cache write
			if userBytes, err := json.Marshal(user); err == nil {
				rdb.Set(c.Request.Context(), syncKey, userBytes, 12*time.Hour)
			}
		}

		// Inject user context for downstream handlers
		c.Set("userID", user.ID)
		c.Set("userEmail", user.Email)
		c.Set("userPlan", user.Plan)
		c.Set("firebaseUID", user.FirebaseUID)

		c.Next()
	}
}

// GetUserID is a helper to safely extract userID from the Gin context.
// Returns ("", false) for anonymous requests.
func GetUserID(c *gin.Context) (string, bool) {
	uid, exists := c.Get("userID")
	if !exists {
		return "", false
	}
	return uid.(string), true
}

// GetUserPlan is a helper to safely extract the user plan from the Gin context.
// Returns PlanFree for anonymous requests.
func GetUserPlan(c *gin.Context) model.Plan {
	plan, exists := c.Get("userPlan")
	if !exists {
		return model.PlanFree
	}
	return plan.(model.Plan)
}

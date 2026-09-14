package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/option"
	"url-shortener/internal/config"
	"url-shortener/internal/database"
	"url-shortener/internal/handler"
	"url-shortener/internal/idgen"
	"url-shortener/internal/middleware"
	"url-shortener/internal/redis"
	"url-shortener/internal/repository"
	"url-shortener/internal/service"
	"url-shortener/internal/worker"
)

func main() {
	slog.Info("Starting Distributed URL Shortener Service...")

	// 1. Load configuration
	cfg := config.LoadConfig()
	slog.Info("Config loaded", "db_host", cfg.DBHost, "db_port", cfg.DBPort, "db_name", cfg.DBName)

	// Set Gin mode
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// 2. Connect to Redis
	rdb, err := redis.NewClient(cfg)
	if err != nil {
		slog.Error("Critical error connecting to Redis", "err", err)
		os.Exit(1)
	}
	slog.Info("Successfully connected to Redis")

	// 3. Connect to PostgreSQL
	db, err := database.NewDB(cfg)
	if err != nil {
		slog.Error("Critical error connecting to Database", "err", err)
		os.Exit(1)
	}
	slog.Info("Successfully connected to Database")

	// 4. Run versioned migrations (golang-migrate).
	//    Applies any pending *.up.sql files from the migrations directory.
	//    Safe to re-run — golang-migrate is idempotent (ErrNoChange is handled).
	if err := db.RunMigrations(cfg.MigrationsPath); err != nil {
		slog.Error("Failed to run database migrations", "err", err)
		os.Exit(1)
	}

	// 5. Initialize Firebase Auth Client
	//    Uses GOOGLE_APPLICATION_CREDENTIALS env var for service account JSON,
	//    or falls back to Application Default Credentials (ADC) in GCP.
	//    If FIREBASE_SERVICE_ACCOUNT_PATH is set, it takes priority.
	var firebaseApp *firebase.App
	saPath := os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH")
	if saPath != "" {
		firebaseApp, err = firebase.NewApp(context.Background(), nil, option.WithCredentialsFile(saPath))
	} else if cfg.FirebaseProjectID != "" {
		// In production on GCP, ADC will auto-resolve.
		// For local dev without a service account file, specify the project ID.
		firebaseApp, err = firebase.NewApp(context.Background(), &firebase.Config{
			ProjectID: cfg.FirebaseProjectID,
		})
	} else {
		firebaseApp, err = firebase.NewApp(context.Background(), nil)
	}
	if err != nil {
		slog.Error("Failed to initialize Firebase app", "err", err)
		os.Exit(1)
	}
	authClient, err := firebaseApp.Auth(context.Background())
	if err != nil {
		slog.Error("Failed to initialize Firebase Auth client", "err", err)
		os.Exit(1)
	}
	slog.Info("Firebase Auth client initialized")

	// 6. Initialize ID Allocator (Range size: 1000)
	allocator := idgen.NewAllocator(rdb, 1000)

	// 7. Initialize Repositories
	urlRepo := repository.NewURLRepository(db)
	userRepo := repository.NewUserRepository(db)
	analyticsRepo := repository.NewAnalyticsRepository(db)

	// 8. Initialize Services
	quotaService := service.NewQuotaService(rdb)
	urlService := service.NewURLService(urlRepo, quotaService, allocator, rdb, cfg.BaseURL)
	userService := service.NewUserService(userRepo, quotaService)
	analyticsService := service.NewAnalyticsService(analyticsRepo, rdb)

	// 9. Initialize Handlers
	h := handler.NewHandler(urlService, analyticsService, userService, rdb, cfg.BaseURL)

	// 10. Start Background Analytics Worker
	workerCtx, workerCancel := context.WithCancel(context.Background())
	// NOTE: workerCancel is NOT deferred here — it must be called explicitly after
	// server.Shutdown() returns, so in-flight requests finish queuing analytics
	// events before the worker stops consuming them.

	analyticsWorker := worker.NewAnalyticsWorker(analyticsRepo, urlRepo, rdb, "queue:analytics", 50, 2*time.Second)
	go analyticsWorker.Start(workerCtx)

	// 11. Setup HTTP Router
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Enable CORS for frontend flexibility.
	// NOTE: AllowOrigins is "*" for local development. This will be scoped to
	// real domains in Level 15 (Security Hardening).
	// NOTE: AllowCredentials MUST NOT be true with AllowOrigins "*" — browsers block it.
	router.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders: []string{"Content-Length"},
		MaxAge:        12 * time.Hour,
	}))

	// Register API Routes
	//
	// Auth strategy:
	//   - OptionalAuth: Token verified if present → sets userID in context.
	//                   No token → proceeds as anonymous.
	//   - RequiredAuth: Token must be present and valid → 401 otherwise.
	//
	optionalAuth := middleware.FirebaseAuth(authClient, userService, rdb, false)
	requiredAuth := middleware.FirebaseAuth(authClient, userService, rdb, true)

	api := router.Group("/api")
	{
		// Public routes with optional auth (anonymous users can still shorten)
		api.POST("/shorten", optionalAuth, middleware.RateLimiter(rdb, 10, time.Minute), h.Shorten)
		api.GET("/analytics/:code", optionalAuth, middleware.RateLimiter(rdb, 30, time.Minute), h.GetAnalytics)
		api.GET("/urls", optionalAuth, middleware.RateLimiter(rdb, 30, time.Minute), h.GetAllURLs)

		// Protected routes (require valid Firebase token)
		api.GET("/me", requiredAuth, h.GetMe)
		api.POST("/claim", requiredAuth, h.ClaimURL)
	}

	// Serve static files if web directory is present (for local running without Nginx)
	if _, err := os.Stat("web"); err == nil {
		router.StaticFile("/", "web/index.html")
		router.StaticFile("/index.html", "web/index.html")
		router.StaticFile("/styles.css", "web/styles.css")
		router.StaticFile("/app.js", "web/app.js")
		router.StaticFile("/auth.js", "web/auth.js")
	} else if _, err := os.Stat("../web"); err == nil {
		router.StaticFile("/", "../web/index.html")
		router.StaticFile("/index.html", "../web/index.html")
		router.StaticFile("/styles.css", "../web/styles.css")
		router.StaticFile("/app.js", "../web/app.js")
		router.StaticFile("/auth.js", "../web/auth.js")
	}

	// Redirection route (public — no auth needed)
	router.GET("/:code", h.Redirect)

	// 12. Graceful HTTP Server Startup
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		slog.Info("HTTP Server listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Failed to listen and serve", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server gracefully...")

	// Timeout context for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "err", err)
		os.Exit(1)
	}
	slog.Info("HTTP server stopped. Draining analytics worker...")

	// Cancel the worker now that the HTTP server is fully stopped —
	// no new analytics events can be enqueued after this point.
	workerCancel()

	// Give the worker up to 3 seconds to flush its current batch to PostgreSQL.
	time.Sleep(3 * time.Second)

	slog.Info("Server exiting")
}

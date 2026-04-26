package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/handlers"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/version"
)

// config gathers everything we read from the environment at startup.
type config struct {
	port           string
	dbPath         string
	migrationsDir  string
	staticDir      string
	jwtSecret      string
	accessTTL      time.Duration
	refreshTTL     time.Duration
	allowedOrigins []string
}

func main() {
	_ = godotenv.Load()
	cfg := loadConfig()

	log.Printf("MedVault %s starting on port %s", version.Version, cfg.port)

	if dir := filepath.Dir(cfg.dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create data directory: %v", err)
		}
	}

	db, err := database.Open(cfg.dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(db, cfg.migrationsDir); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           buildRouter(db, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()
	log.Printf("ready: listening on :%s", cfg.port)

	// Wait for SIGINT/SIGTERM, then drain connections within 10s.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("shutdown signal received")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	log.Println("bye")
}

func loadConfig() config {
	c := config{
		port:          getEnv("PORT", "8080"),
		dbPath:        getEnv("DB_PATH", "./data/medvault.db"),
		migrationsDir: getEnv("MIGRATIONS_DIR", "./migrations"),
		staticDir:     getEnv("STATIC_DIR", "./static"),
		jwtSecret:     os.Getenv("JWT_SECRET"),
		accessTTL:     parseDuration("JWT_ACCESS_TTL", 15*time.Minute),
		refreshTTL:    parseDuration("JWT_REFRESH_TTL", 48*time.Hour),
	}
	if c.jwtSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}

	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		c.allowedOrigins = strings.Split(origins, ",")
	} else {
		// Dev default: Vite's dev server.
		c.allowedOrigins = []string{"http://localhost:5173"}
	}

	return c
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Printf("warning: %s is not a valid duration; using default %s", key, fallback)
	}
	return fallback
}

// buildRouter wires every route, middleware, and rate limiter.
//
// Layering rules:
//   - Logging + CORS apply to every request.
//   - /api/auth/* is public but IP-rate-limited per SECURITY.md.
//   - Everything else under /api/* is JWT-protected.
//   - /api/sync gets its own stricter user-rate-limit (30/min).
//   - /api/settings is per-user; it does NOT use the household middleware.
//   - All other protected routes get the standard 60/min user limit AND
//     the household middleware (which injects household_id into context).
//   - Anything not matched falls through to the SPA handler so React Router
//     can take over for client-side routes.
func buildRouter(db *sql.DB, cfg config) http.Handler {
	authH := &handlers.AuthHandler{
		DB:         db,
		JWTSecret:  cfg.jwtSecret,
		AccessTTL:  cfg.accessTTL,
		RefreshTTL: cfg.refreshTTL,
	}
	medH := &handlers.MedicineHandler{DB: db}
	hhH := &handlers.HouseholdHandler{DB: db}
	syncH := &handlers.SyncHandler{DB: db}
	alertH := &handlers.AlertsHandler{DB: db}
	setH := &handlers.SettingsHandler{DB: db}

	registerLimit := middleware.NewLimiter(5, middleware.ByIP)
	loginLimit := middleware.NewLimiter(10, middleware.ByIP)
	refreshLimit := middleware.NewLimiter(20, middleware.ByIP)
	syncLimit := middleware.NewLimiter(30, middleware.ByUser)
	apiLimit := middleware.NewLimiter(60, middleware.ByUser)

	r := chi.NewRouter()
	r.Use(middleware.Logging)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.CORS(cfg.allowedOrigins))

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/auth", func(r chi.Router) {
		r.With(registerLimit.Middleware).Post("/register", authH.Register)
		r.With(loginLimit.Middleware).Post("/login", authH.Login)
		r.With(refreshLimit.Middleware).Post("/refresh", authH.Refresh)
		r.With(loginLimit.Middleware).Post("/logout", authH.Logout)
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(cfg.jwtSecret))

		// Sync: stricter rate limit, household-scoped.
		r.With(syncLimit.Middleware, middleware.RequireHousehold(db)).
			Post("/api/sync", syncH.Sync)

		// Settings: per-user, NO household middleware.
		r.With(apiLimit.Middleware).Route("/api/settings", func(r chi.Router) {
			r.Get("/", setH.GetSettings)
			r.Put("/", setH.UpdateSettings)
		})

		// Everything else: standard rate limit + household scoping.
		r.Group(func(r chi.Router) {
			r.Use(apiLimit.Middleware)
			r.Use(middleware.RequireHousehold(db))

			r.Get("/api/alerts", alertH.Alerts)

			r.Route("/api/medicines", func(r chi.Router) {
				r.Get("/", medH.List)
				r.Post("/", medH.Create)
				r.Get("/{id}", medH.Get)
				r.Put("/{id}", medH.Update)
				r.Delete("/{id}", medH.Delete)
			})

			r.Route("/api/households", func(r chi.Router) {
				r.Get("/current", hhH.GetCurrent)
				r.Post("/join", hhH.Join)
				r.Post("/{id}/invite", hhH.CreateInvite)
				r.Delete("/{id}/members/{user_id}", hhH.RemoveMember)
			})
		})
	})

	r.Handle("/*", spaHandler(cfg.staticDir))

	return r
}

// spaHandler serves the React SPA from staticDir.
//
// If the requested file exists on disk, it's served verbatim. Otherwise we
// return index.html so React Router can resolve the path on the client.
//
// Two guard rails:
//   - /api/* never falls back — those paths get a real 404, not the SPA.
//   - filepath.Clean strips ".." so a malicious path can't escape staticDir.
func spaHandler(staticDir string) http.Handler {
	fs := http.FileServer(http.Dir(staticDir))
	indexPath := filepath.Join(staticDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		path := filepath.Join(staticDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}

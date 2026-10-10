package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
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
	port             string
	dbPath           string
	migrationsDir    string
	staticDir        string
	jwtSecret        string
	accessTTL        time.Duration
	refreshTTL       time.Duration
	allowedOrigins   []string
	trustedProxies   []netip.Prefix // peers allowed to set X-Forwarded-For
	drugDBDir        string         // base dir for per-country drug DBs; empty = disabled
	drugDBCountry    string         // active country code for sync-time enrichment
	photoDir         string         // where uploaded medicine photos live on disk
	backupDir        string         // empty = scheduled backups disabled
	backupRetainDays int            // 0 = keep forever
	backupHour       int            // 0..23 local time, default 3
	registrationMode string         // open | invite | closed
}

func main() {
	_ = godotenv.Load()

	if len(os.Args) > 1 {
		if err := runAdmin(context.Background(), os.Args[1:], os.Getenv, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg := loadConfig()

	log.Printf("MedVault %s starting on port %s", version.Version, cfg.port)

	if dir := filepath.Dir(cfg.dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Fatalf("create data directory: %v", err)
		}
	}

	db, err := database.Open(cfg.dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}

	if err := database.Migrate(db, cfg.migrationsDir); err != nil {
		_ = db.Close()
		log.Fatalf("migrate: %v", err)
	}
	defer db.Close() //nolint:errcheck // best-effort cleanup on shutdown

	drugReg := database.NewDrugRegistry(cfg.drugDBDir)
	if cs := drugReg.Countries(); len(cs) > 0 {
		log.Printf("drug DBs registered (lazy-open): %v; sync country: %s", cs, cfg.drugDBCountry)
	} else {
		log.Printf("drug DBs: none found under %s (autofill disabled)", cfg.drugDBDir)
	}
	defer drugReg.Close() //nolint:errcheck // best-effort cleanup on shutdown

	stopBackups := database.StartBackupScheduler(db, database.BackupConfig{
		Dir:        cfg.backupDir,
		RetainDays: cfg.backupRetainDays,
		Hour:       cfg.backupHour,
	})
	defer stopBackups()

	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           buildRouter(db, drugReg, &cfg),
		ReadHeaderTimeout: 10 * time.Second,
		// WriteTimeout caps total response time; IdleTimeout cleans up
		// keep-alive connections that go quiet. Both protect against slow
		// or wedged clients holding sockets open.
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
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
		port:             getEnv("PORT", "8080"),
		dbPath:           getEnv("DB_PATH", "./data/medvault.db"),
		migrationsDir:    getEnv("MIGRATIONS_DIR", "./migrations"),
		staticDir:        getEnv("STATIC_DIR", "./static"),
		jwtSecret:        os.Getenv("JWT_SECRET"),
		accessTTL:        parseDuration("JWT_ACCESS_TTL", 15*time.Minute),
		refreshTTL:       parseDuration("JWT_REFRESH_TTL", 48*time.Hour),
		drugDBDir:        getEnv("DRUG_DB_DIR", "./data"),
		drugDBCountry:    strings.ToLower(getEnv("DRUG_DB_COUNTRY", "ua")),
		photoDir:         getEnv("PHOTO_DIR", "./data/photos"),
		backupDir:        os.Getenv("BACKUP_DIR"), // empty disables
		backupRetainDays: parseIntEnv("BACKUP_RETAIN_DAYS", 14),
		backupHour:       parseIntEnv("BACKUP_HOUR", 3),
		registrationMode: strings.ToLower(getEnv("REGISTRATION_MODE", handlers.RegistrationOpen)),
	}
	if c.jwtSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}
	switch c.registrationMode {
	case handlers.RegistrationOpen, handlers.RegistrationInvite, handlers.RegistrationClosed:
	default:
		log.Fatalf("REGISTRATION_MODE: want open, invite or closed, got %q", c.registrationMode)
	}

	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		// Trim each entry: "a, b" must yield ["a", "b"], not ["a", " b"].
		// A " b" entry would never match a browser Origin header.
		for _, o := range strings.Split(origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				c.allowedOrigins = append(c.allowedOrigins, o)
			}
		}
	}
	c.trustedProxies = parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))

	if len(c.allowedOrigins) == 0 {
		// Dev default: Vite's dev server.
		c.allowedOrigins = []string{"http://localhost:5173"}
	}

	return c
}

// parseTrustedProxies reads a comma-separated list of IPs or CIDRs. A typo
// here would silently disable per-client rate limiting (or trust the wrong
// peer), so invalid entries are fatal.
func parseTrustedProxies(raw string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			log.Fatalf("TRUSTED_PROXIES: invalid IP or CIDR %q", s)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("warning: %s is not a valid integer; using default %d", key, fallback)
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
func buildRouter(db *sql.DB, drugReg *database.DrugRegistry, cfg *config) http.Handler {
	authH := &handlers.AuthHandler{
		DB:               db,
		JWTSecret:        cfg.jwtSecret,
		AccessTTL:        cfg.accessTTL,
		RefreshTTL:       cfg.refreshTTL,
		RegistrationMode: cfg.registrationMode,
	}
	medH := &handlers.MedicineHandler{DB: db}
	hhH := &handlers.HouseholdHandler{DB: db}
	syncH := &handlers.SyncHandler{
		DB:           db,
		DrugRegistry: drugReg,
		DrugCountry:  cfg.drugDBCountry,
	}
	alertH := &handlers.AlertsHandler{DB: db}
	setH := &handlers.SettingsHandler{DB: db}
	drugH := &handlers.DrugHandler{Registry: drugReg}
	photoH := &handlers.PhotoHandler{DB: db, PhotoDir: cfg.photoDir}
	auditH := &handlers.AuditHandler{DB: db}

	registerLimit := middleware.NewLimiter(5, middleware.ByIP)
	loginLimit := middleware.NewLimiter(10, middleware.ByIP)
	refreshLimit := middleware.NewLimiter(20, middleware.ByIP)
	drugLimit := middleware.NewLimiter(60, middleware.ByIP)
	syncLimit := middleware.NewLimiter(30, middleware.ByUser)
	apiLimit := middleware.NewLimiter(60, middleware.ByUser)

	r := chi.NewRouter()
	// Order matters: Recover must wrap everything so a panic in any later
	// middleware or handler still produces a 500 envelope. RequestID comes
	// next so Logging (and Recover's own log line) can include the req_id.
	// RealIP must precede Logging and every IP-keyed rate limiter.
	r.Use(middleware.Recover)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP(cfg.trustedProxies))
	r.Use(middleware.Logging)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.CORS(cfg.allowedOrigins))

	// Liveness: process is up. Cheap, never touches the DB.
	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Readiness: process can serve real traffic. Pings the DB so a load
	// balancer can drain us if SQLite is wedged.
	r.Get("/api/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			api.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "database not ready")
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// Drug autofill: public read-only endpoint (no auth required — drug DB is
	// public pharmaceutical data). Rate-limited by IP.
	r.With(drugLimit.Middleware).Get("/api/drugs/search", drugH.Search)

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

		// Account management: per-user, no household middleware.
		r.With(apiLimit.Middleware).Post("/api/auth/password", authH.ChangePassword)
		r.With(apiLimit.Middleware).Post("/api/auth/logout-others", authH.LogoutOthers)
		r.With(apiLimit.Middleware).Delete("/api/auth/account", authH.DeleteAccount)

		// Settings: per-user, NO household middleware.
		r.With(apiLimit.Middleware).Route("/api/settings", func(r chi.Router) {
			r.Get("/", setH.GetSettings)
			r.Put("/", setH.UpdateSettings)
		})

		// Medicines + alerts: standard rate limit + household scoping.
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
				r.Get("/{id}/history", auditH.MedicineHistory)

				// Photos travel out-of-band: no rate-limit override, but
				// the upload path uses MaxBytesReader for body protection.
				r.Get("/{id}/photo", photoH.Get)
				r.Post("/{id}/photo", photoH.Upload)
				r.Delete("/{id}/photo", photoH.Delete)
			})
		})

		// All household routes in one block. The membership-management
		// routes (List, Create, Switch, Join) intentionally skip
		// RequireHousehold so a user who just lost their only household can
		// still recover.
		r.Route("/api/households", func(r chi.Router) {
			r.With(apiLimit.Middleware).Get("/", hhH.List)
			r.With(apiLimit.Middleware).Post("/", hhH.Create)
			r.With(apiLimit.Middleware).Post("/switch", hhH.Switch)
			r.With(apiLimit.Middleware).Post("/join", hhH.Join)

			r.Group(func(r chi.Router) {
				r.Use(apiLimit.Middleware)
				r.Use(middleware.RequireHousehold(db))
				r.Get("/current", hhH.GetCurrent)
				r.Patch("/{id}", hhH.Rename)
				r.Delete("/{id}/membership", hhH.Leave)
				r.Post("/{id}/transfer", hhH.Transfer)
				r.Post("/{id}/invite", hhH.CreateInvite)
				r.Get("/{id}/invites", hhH.ListInvites)
				r.Delete("/{id}/invites/{code}", hhH.RevokeInvite)
				r.Delete("/{id}/members/{user_id}", hhH.RemoveMember)
				r.Get("/{id}/audit", auditH.List)
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
//   - URL paths use forward slashes, so we use path.Clean (not filepath.Clean)
//     to resolve "..": filepath.Clean would interpret backslashes on Windows.
func spaHandler(staticDir string) http.Handler {
	fs := http.FileServer(http.Dir(staticDir))
	indexPath := filepath.Join(staticDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		clean := path.Clean(r.URL.Path)
		fsPath := filepath.Join(staticDir, filepath.FromSlash(clean))
		//nolint:gosec // clean is rooted, so fsPath stays under staticDir
		if info, err := os.Stat(fsPath); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}

package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goblog/core/handlers"
	"goblog/core/models"
	"goblog/core/plugin"
	"goblog/core/services"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	cfg := loadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := openDB(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	serviceDB := services.DB(services.NewSQLDB(db, cfg.DBDriver))
	if cfg.DBReadDSN != "" {
		readDB, err := openDB(config{DBDriver: cfg.DBDriver, DBDSN: cfg.DBReadDSN})
		if err != nil {
			log.Fatalf("read database health check failed: %v", err)
		}
		defer readDB.Close()
		serviceDB = services.NewDBRouter(db, readDB, cfg.DBDriver)
	}

	if err := models.Migrate(ctx, db, cfg.DBDriver); err != nil {
		log.Fatal(err)
	}
	setupCtx := services.WithWriter(ctx)

	options := services.NewOptionService(serviceDB)
	if err := options.EnsureDefaults(setupCtx); err != nil {
		log.Fatal(err)
	}

	users := services.NewUserService(serviceDB)
	userCount, err := users.Count(setupCtx)
	if err != nil {
		log.Fatal(err)
	}
	defaultAdminReady := false
	if shouldCreateDefaultAdmin(userCount, cfg) {
		if err := users.EnsureDefaultAdmin(setupCtx, cfg.AdminUser, cfg.AdminPassword, cfg.AdminMail); err != nil {
			log.Fatal(err)
		}
		defaultAdminReady = true
	} else if userCount == 0 {
		log.Printf("web install is available at http://localhost%s/install", cfg.Addr)
	} else {
		defaultAdminReady = true
	}

	contents := services.NewContentService(serviceDB)
	metas := services.NewMetaService(serviceDB)
	if err := metas.EnsureDefaultCategory(setupCtx); err != nil {
		log.Fatal(err)
	}
	comments := services.NewCommentService(serviceDB)
	app := handlers.New(contents, metas, comments, users, options, plugin.Default)

	log.Printf("goblog listening on %s", cfg.Addr)
	if defaultAdminReady {
		log.Printf("admin: http://localhost%s/admin", cfg.Addr)
	}
	if err := http.ListenAndServe(cfg.Addr, app.Handler()); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type config struct {
	Addr          string
	DBDriver      string
	DBDSN         string
	DBReadDSN     string
	DBWriteDSN    string
	AdminUser     string
	AdminPassword string
	AdminMail     string
	WebInstall    bool
	AdminExplicit bool
}

func loadConfig() config {
	driver := os.Getenv("GOBLOG_DB_DRIVER")
	dsn := os.Getenv("GOBLOG_DB_DSN")
	if driver == "" {
		driver = chooseDriver()
	}
	if dsn == "" && (driver == "sqlite" || driver == "sqlite3") {
		dsn = filepath.Join("data", "goblog.db")
	}
	writeDSN := os.Getenv("GOBLOG_DB_WRITE_DSN")
	if writeDSN != "" {
		dsn = writeDSN
	}

	_, adminUserSet := os.LookupEnv("GOBLOG_ADMIN_USER")
	_, adminPasswordSet := os.LookupEnv("GOBLOG_ADMIN_PASSWORD")
	_, adminMailSet := os.LookupEnv("GOBLOG_ADMIN_MAIL")

	return config{
		Addr:          env("GOBLOG_ADDR", ":8080"),
		DBDriver:      driver,
		DBDSN:         dsn,
		DBReadDSN:     os.Getenv("GOBLOG_DB_READ_DSN"),
		DBWriteDSN:    writeDSN,
		AdminUser:     env("GOBLOG_ADMIN_USER", "admin"),
		AdminPassword: env("GOBLOG_ADMIN_PASSWORD", "admin123"),
		AdminMail:     env("GOBLOG_ADMIN_MAIL", "admin@example.com"),
		WebInstall:    envBool("GOBLOG_WEB_INSTALL", true),
		AdminExplicit: adminUserSet || adminPasswordSet || adminMailSet,
	}
}

func shouldCreateDefaultAdmin(userCount int, cfg config) bool {
	if userCount > 0 {
		return false
	}
	return !cfg.WebInstall || cfg.AdminExplicit
}

func chooseDriver() string {
	defaultDSN := filepath.Join("data", "goblog.db")
	if _, err := os.Stat(defaultDSN); err == nil {
		return "sqlite"
	}
	info, err := os.Stdin.Stat()
	if err != nil || (info.Mode()&os.ModeCharDevice) == 0 {
		return "sqlite"
	}
	fmt.Print("首次启动，请选择数据库后端 [sqlite/mariadb/mysql/postgres]，默认 sqlite: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "mariadb":
		return "mariadb"
	case "mysql":
		return "mysql"
	case "postgres", "postgresql", "pgx":
		return "postgres"
	default:
		return "sqlite"
	}
}

func openDB(cfg config) (*sql.DB, error) {
	driver := cfg.DBDriver
	if driver == "sqlite" {
		driver = "sqlite3"
		if err := os.MkdirAll(filepath.Dir(cfg.DBDSN), 0755); err != nil {
			return nil, err
		}
	}
	if driver == "mariadb" {
		driver = "mysql"
	}
	if driver == "postgresql" || driver == "pgx" {
		driver = "postgres"
	}
	db, err := sql.Open(driver, cfg.DBDSN)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite3" && !strings.Contains(cfg.DBDSN, "?") {
		db.SetMaxOpenConns(1)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

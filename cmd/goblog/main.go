package main

import (
	"context"
	"database/sql"
	"errors"
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

	if err := models.Migrate(ctx, db, cfg.DBDriver); err != nil {
		log.Fatal(err)
	}

	options := services.NewOptionService(db)
	if err := options.EnsureDefaults(ctx); err != nil {
		log.Fatal(err)
	}

	users := services.NewUserService(db)
	if err := users.EnsureDefaultAdmin(ctx, cfg.AdminUser, cfg.AdminPassword, cfg.AdminMail); err != nil {
		log.Fatal(err)
	}

	contents := services.NewContentService(db)
	app := handlers.New(contents, users, options, plugin.Default)

	log.Printf("goblog listening on %s", cfg.Addr)
	log.Printf("admin: http://localhost%s/admin (default %s/%s)", cfg.Addr, cfg.AdminUser, cfg.AdminPassword)
	if err := http.ListenAndServe(cfg.Addr, app.Handler()); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type config struct {
	Addr          string
	DBDriver      string
	DBDSN         string
	AdminUser     string
	AdminPassword string
	AdminMail     string
}

func loadConfig() config {
	driver := env("GOBLOG_DB_DRIVER", "sqlite")
	dsn := os.Getenv("GOBLOG_DB_DSN")
	if dsn == "" && (driver == "sqlite" || driver == "sqlite3") {
		dsn = filepath.Join("data", "goblog.db")
	}

	return config{
		Addr:          env("GOBLOG_ADDR", ":8080"),
		DBDriver:      driver,
		DBDSN:         dsn,
		AdminUser:     env("GOBLOG_ADMIN_USER", "admin"),
		AdminPassword: env("GOBLOG_ADMIN_PASSWORD", "admin123"),
		AdminMail:     env("GOBLOG_ADMIN_MAIL", "admin@example.com"),
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

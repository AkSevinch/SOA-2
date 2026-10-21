package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/seva/marketplace/internal/apierr"
	"github.com/seva/marketplace/internal/auth"
	"github.com/seva/marketplace/internal/db"
	"github.com/seva/marketplace/internal/httpserver"
	"github.com/seva/marketplace/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func main() {
	addr := env("HTTP_ADDR", ":8080")
	databaseURL := env("DATABASE_URL", "postgres://marketplace:marketplace@localhost:5432/marketplace")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		slog.Error("подключение к БД", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	products := store.NewProductStore(pool)
	// N минут между операциями с заказами (0 отключает ограничение).
	rateLimit := time.Duration(envInt("ORDER_RATE_LIMIT_MINUTES", 1)) * time.Minute
	orders := store.NewOrderStore(pool, rateLimit)
	users := store.NewUserStore(pool)
	promos := store.NewPromoCodeStore(pool)

	accessTTL := time.Duration(envInt("ACCESS_TOKEN_TTL_MINUTES", 20)) * time.Minute
	refreshTTL := time.Duration(envInt("REFRESH_TOKEN_TTL_DAYS", 14)) * 24 * time.Hour
	tokens := auth.NewTokenService(env("JWT_SECRET", "dev-secret-change-me"), accessTTL, refreshTTL)

	handler := httpserver.NewHandler(products, orders, users, promos, tokens)

	strict := httpserver.NewStrictHandlerWithOptions(handler, nil, httpserver.StrictHTTPServerOptions{
		// Ошибка разбора тела/параметров запроса → 400 VALIDATION_ERROR (п.5/6)
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("ошибка разбора запроса", "error", err)
			httpserver.WriteError(w, apierr.Validation(err.Error(), nil))
		},
		// Неожиданная ошибка обработчика → 500 INTERNAL_ERROR (без утечки деталей)
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("внутренняя ошибка обработчика", "error", err)
			httpserver.WriteError(w, apierr.New(apierr.InternalError, "внутренняя ошибка сервера"))
		},
	})

	// Контрактная валидация: запросы, не соответствующие OpenAPI-спеке,
	// отклоняются до бизнес-логики (п.6).
	validator, err := httpserver.NewRequestValidator()
	if err != nil {
		slog.Error("валидатор запросов", "error", err)
		os.Exit(1)
	}

	corsOrigins := strings.Split(env("CORS_ALLOWED_ORIGINS", "http://localhost:8081"), ",")

	r := chi.NewRouter()
	r.Use(httpserver.CORS(corsOrigins))
	r.Use(httpserver.RequestLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(httpserver.AuthMiddleware(tokens))
	r.Use(validator)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		httpserver.WriteError(w, apierr.New(apierr.NotFound, "эндпоинт не найден"))
	})

	httpserver.HandlerWithOptions(strict, httpserver.ChiServerOptions{
		BaseRouter: r,
		// Ошибка биндинга параметров (например, невалидный UUID в пути) → 400 VALIDATION_ERROR
		ErrorHandlerFunc: func(w http.ResponseWriter, req *http.Request, err error) {
			slog.Error("ошибка биндинга параметров", "error", err)
			httpserver.WriteError(w, apierr.Validation(err.Error(), nil))
		},
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	slog.Info("server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server", "error", err)
		os.Exit(1)
	}
}

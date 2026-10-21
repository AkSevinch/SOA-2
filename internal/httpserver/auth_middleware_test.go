package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/seva/marketplace/internal/auth"
)

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) Error {
	t.Helper()
	var e Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("ответ не JSON-ошибка: %v\n%s", err, rec.Body.String())
	}
	return e
}

func TestAuthMiddlewareMissingToken(t *testing.T) {
	tokens := auth.NewTokenService("secret", time.Minute, time.Hour)
	h := AuthMiddleware(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("хендлер не должен вызываться")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := decodeError(t, rec).ErrorCode; got != TOKENINVALID {
		t.Fatalf("error_code = %s", got)
	}
}

func TestAuthMiddlewareExpiredToken(t *testing.T) {
	tokens := auth.NewTokenService("secret", -time.Minute, -time.Minute)
	pair, err := tokens.Generate("user-1", "USER")
	if err != nil {
		t.Fatal(err)
	}
	h := AuthMiddleware(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("хендлер не должен вызываться")
	}))
	req := httptest.NewRequest(http.MethodGet, "/orders/1", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := decodeError(t, rec).ErrorCode; got != TOKENEXPIRED {
		t.Fatalf("error_code = %s", got)
	}
}

func TestAuthMiddlewareValidToken(t *testing.T) {
	tokens := auth.NewTokenService("secret", time.Minute, time.Hour)
	pair, err := tokens.Generate("user-1", "SELLER")
	if err != nil {
		t.Fatal(err)
	}
	var userID, role string
	h := AuthMiddleware(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID = UserIDFromContext(r.Context())
		role = RoleFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/products", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if userID != "user-1" || role != "SELLER" {
		t.Fatalf("userID=%q role=%q", userID, role)
	}
}

func TestAuthMiddlewarePublicPath(t *testing.T) {
	tokens := auth.NewTokenService("secret", time.Minute, time.Hour)
	h := AuthMiddleware(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAuthMiddlewarePromoCodesRequiresToken(t *testing.T) {
	tokens := auth.NewTokenService("secret", time.Minute, time.Hour)
	h := AuthMiddleware(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("хендлер не должен вызываться без токена")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/promo-codes", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

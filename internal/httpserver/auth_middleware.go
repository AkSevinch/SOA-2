package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/seva/marketplace/internal/apierr"
	"github.com/seva/marketplace/internal/auth"
)

type ctxKeyRole struct{}

func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, ctxKeyRole{}, role)
}

func RoleFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	role, _ := ctx.Value(ctxKeyRole{}).(string)
	return role
}

func requiresAuth(path string) bool {
	return strings.HasPrefix(path, "/products") ||
		strings.HasPrefix(path, "/orders") ||
		strings.HasPrefix(path, "/promo-codes")
}

func AuthMiddleware(tokens *auth.TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !requiresAuth(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			const prefix = "Bearer "
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, prefix) {
				WriteError(w, apierr.New(apierr.TokenInvalid, "Access token невалиден"))
				return
			}

			claims, err := tokens.ParseAccess(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
			if err != nil {
				if errors.Is(err, auth.ErrTokenExpired) {
					WriteError(w, apierr.New(apierr.TokenExpired, "Access token истёк"))
					return
				}
				WriteError(w, apierr.New(apierr.TokenInvalid, "Access token невалиден"))
				return
			}

			ctx := SetUserID(r.Context(), claims.Subject)
			ctx = WithRole(ctx, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

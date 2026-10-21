package httpserver

import (
	"context"

	"github.com/seva/marketplace/internal/apierr"
)

func accessDenied() *apierr.Error {
	return apierr.New(apierr.AccessDenied, "Недостаточно прав")
}

func isAdmin(ctx context.Context) bool {
	return RoleFromContext(ctx) == "ADMIN"
}

func requireRole(ctx context.Context, roles ...string) *apierr.Error {
	role := RoleFromContext(ctx)
	for _, allowed := range roles {
		if role == allowed {
			return nil
		}
	}
	return accessDenied()
}

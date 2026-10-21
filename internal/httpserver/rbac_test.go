package httpserver

import (
	"context"
	"testing"

	"github.com/seva/marketplace/internal/apierr"
	"github.com/seva/marketplace/internal/store"
)

func TestRequireRole(t *testing.T) {
	ctx := WithRole(context.Background(), "USER")
	if err := requireRole(ctx, "USER", "ADMIN"); err != nil {
		t.Fatalf("USER должен иметь доступ: %v", err)
	}
	err := requireRole(ctx, "SELLER", "ADMIN")
	if err == nil || err.Code != apierr.AccessDenied {
		t.Fatalf("ожидался ACCESS_DENIED, получено %v", err)
	}
}

func TestEnsureProductOwnership(t *testing.T) {
	sellerID := "seller-1"
	product := &store.Product{SellerID: &sellerID}

	ctx := WithUserID(WithRole(context.Background(), "SELLER"), "seller-1")
	if err := ensureProductOwnership(ctx, product); err != nil {
		t.Fatalf("владелец должен иметь доступ: %v", err)
	}

	other := WithUserID(WithRole(context.Background(), "SELLER"), "seller-2")
	if err := ensureProductOwnership(other, product); err == nil || err.Code != apierr.AccessDenied {
		t.Fatalf("чужой товар: ожидался ACCESS_DENIED, получено %v", err)
	}

	admin := WithUserID(WithRole(context.Background(), "ADMIN"), "admin-1")
	if err := ensureProductOwnership(admin, &store.Product{}); err != nil {
		t.Fatalf("ADMIN должен обходить проверку владельца: %v", err)
	}
}

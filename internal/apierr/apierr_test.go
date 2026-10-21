package apierr

import (
	"net/http"
	"testing"
)

func TestHTTPStatusCatalog(t *testing.T) {
	cases := []struct {
		code   Code
		status int
	}{
		{ValidationError, http.StatusBadRequest},
		{TokenExpired, http.StatusUnauthorized},
		{TokenInvalid, http.StatusUnauthorized},
		{RefreshTokenInvalid, http.StatusUnauthorized},
		{OrderOwnershipViolation, http.StatusForbidden},
		{AccessDenied, http.StatusForbidden},
		{ProductNotFound, http.StatusNotFound},
		{OrderNotFound, http.StatusNotFound},
		{NotFound, http.StatusNotFound},
		{ProductInactive, http.StatusConflict},
		{OrderHasActive, http.StatusConflict},
		{InvalidStateTransition, http.StatusConflict},
		{InsufficientStock, http.StatusConflict},
		{PromoCodeInvalid, http.StatusUnprocessableEntity},
		{PromoCodeMinAmount, http.StatusUnprocessableEntity},
		{OrderLimitExceeded, http.StatusTooManyRequests},
		{InternalError, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		e := New(tc.code, "test")
		if got := e.HTTPStatus(); got != tc.status {
			t.Errorf("%s: статус %d, ожидался %d", tc.code, got, tc.status)
		}
	}
}

func TestValidationDetails(t *testing.T) {
	e := Validation("Ошибка валидации", map[string]interface{}{"price": "должно быть >= 0.01"})
	if e.Code != ValidationError {
		t.Fatalf("код %q, ожидался %q", e.Code, ValidationError)
	}
	if e.HTTPStatus() != http.StatusBadRequest {
		t.Fatalf("статус %d, ожидался 400", e.HTTPStatus())
	}
	if e.Details["price"] != "должно быть >= 0.01" {
		t.Fatalf("details не сохранились: %v", e.Details)
	}
}

func TestWithFieldDetails(t *testing.T) {
	e := New(ValidationError, "Ошибка валидации").
		WithFieldDetails("name", "длина должна быть от 1 до 255").
		WithFieldDetails("stock", "должно быть >= 0")
	if len(e.Details) != 2 {
		t.Fatalf("ожидалось 2 нарушения, получено %v", e.Details)
	}
}

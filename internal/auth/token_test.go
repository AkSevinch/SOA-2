package auth

import (
	"errors"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "secret123" {
		t.Fatal("пароль хранится в открытом виде")
	}
	if !CheckPassword(hash, "secret123") {
		t.Fatal("верный пароль не принят")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("неверный пароль принят")
	}
}

func TestTokenPair(t *testing.T) {
	svc := NewTokenService("test-secret", 20*time.Minute, 14*24*time.Hour)
	pair, err := svc.Generate("user-1", "USER")
	if err != nil {
		t.Fatal(err)
	}
	if pair.ExpiresIn != 20*time.Minute {
		t.Fatalf("ExpiresIn = %v", pair.ExpiresIn)
	}

	claims, err := svc.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.Role != "USER" || claims.Type != TypeAccess {
		t.Fatalf("claims = %+v", claims)
	}

	refresh, err := svc.ParseRefresh(pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if refresh.Subject != "user-1" || refresh.Type != TypeRefresh {
		t.Fatalf("refresh claims = %+v", refresh)
	}
}

func TestTokenTypeMismatch(t *testing.T) {
	svc := NewTokenService("test-secret", 20*time.Minute, 14*24*time.Hour)
	pair, err := svc.Generate("user-1", "USER")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseAccess(pair.RefreshToken); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("refresh token принят как access: %v", err)
	}
	if _, err := svc.ParseRefresh(pair.AccessToken); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("access token принят как refresh: %v", err)
	}
}

func TestTokenExpired(t *testing.T) {
	svc := NewTokenService("test-secret", -time.Minute, -time.Minute)
	pair, err := svc.Generate("user-1", "USER")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseAccess(pair.AccessToken); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("ожидался ErrTokenExpired, получено %v", err)
	}
}

func TestTokenWrongSecret(t *testing.T) {
	issuer := NewTokenService("secret-a", time.Minute, time.Hour)
	pair, err := issuer.Generate("user-1", "USER")
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewTokenService("secret-b", time.Minute, time.Hour)
	if _, err := verifier.ParseAccess(pair.AccessToken); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("ожидался ErrTokenInvalid, получено %v", err)
	}
}

func TestTokenGarbage(t *testing.T) {
	svc := NewTokenService("test-secret", time.Minute, time.Hour)
	if _, err := svc.ParseAccess("not-a-token"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("ожидался ErrTokenInvalid, получено %v", err)
	}
}

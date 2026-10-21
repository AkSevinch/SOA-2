package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

const (
	TypeAccess  = "access"
	TypeRefresh = "refresh"
)

type Claims struct {
	Role string `json:"role"`
	Type string `json:"typ"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

type TokenService struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

func NewTokenService(secret string, accessTTL, refreshTTL time.Duration) *TokenService {
	return &TokenService{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "marketplace",
	}
}

func (s *TokenService) AccessTTL() time.Duration {
	return s.accessTTL
}

func (s *TokenService) Generate(userID, role string) (TokenPair, error) {
	now := time.Now()
	sign := func(typ string, ttl time.Duration) (string, error) {
		claims := Claims{
			Role: role,
			Type: typ,
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   userID,
				Issuer:    s.issuer,
				IssuedAt:  jwt.NewNumericDate(now),
				ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
				ID:        uuid.NewString(),
			},
		}
		return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	}

	access, err := sign(TypeAccess, s.accessTTL)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := sign(TypeRefresh, s.refreshTTL)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresIn: s.accessTTL}, nil
}

func (s *TokenService) ParseAccess(raw string) (*Claims, error) {
	return s.parse(raw, TypeAccess)
}

func (s *TokenService) ParseRefresh(raw string) (*Claims, error) {
	return s.parse(raw, TypeRefresh)
}

func (s *TokenService) parse(raw, typ string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(s.issuer))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if !token.Valid || claims.Type != typ || claims.Subject == "" {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

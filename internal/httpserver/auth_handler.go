package httpserver

import (
	"context"
	"errors"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/seva/marketplace/internal/apierr"
	"github.com/seva/marketplace/internal/auth"
	"github.com/seva/marketplace/internal/store"
)

func toUserResponse(u *store.User) UserResponse {
	return UserResponse{
		Id:        uuid.MustParse(u.ID),
		Email:     openapi_types.Email(u.Email),
		Role:      UserRole(u.Role),
		CreatedAt: u.CreatedAt,
	}
}

func tokenPair(pair auth.TokenPair) TokenPair {
	return TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    TokenPairTokenTypeBearer,
		ExpiresIn:    int64(pair.ExpiresIn.Seconds()),
	}
}

func authResponse(u *store.User, pair auth.TokenPair) AuthResponse {
	return AuthResponse{
		User:         toUserResponse(u),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    AuthResponseTokenTypeBearer,
		ExpiresIn:    int64(pair.ExpiresIn.Seconds()),
	}
}

func invalidCredentials() *apierr.Error {
	return apierr.New(apierr.InvalidCredentials, "Неверный email или пароль")
}

func refreshTokenInvalid() *apierr.Error {
	return apierr.New(apierr.RefreshTokenInvalid, "Refresh token невалиден")
}

func (h *APIHandler) RegisterUser(ctx context.Context, request RegisterUserRequestObject) (RegisterUserResponseObject, error) {
	body := request.Body
	if body == nil {
		return RegisterUser400JSONResponse{badRequest(apierr.Validation("тело запроса обязательно", nil))}, nil
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		return nil, err
	}

	user, err := h.users.Create(ctx, string(body.Email), hash, "USER")
	if errors.Is(err, store.ErrEmailTaken) {
		return RegisterUser409JSONResponse{emailAlreadyExists(apierr.New(
			apierr.EmailAlreadyExists, "Пользователь с таким email уже зарегистрирован"))}, nil
	}
	if err != nil {
		return nil, err
	}

	pair, err := h.tokens.Generate(user.ID, user.Role)
	if err != nil {
		return nil, err
	}
	return RegisterUser201JSONResponse(authResponse(user, pair)), nil
}

func (h *APIHandler) LoginUser(ctx context.Context, request LoginUserRequestObject) (LoginUserResponseObject, error) {
	body := request.Body
	if body == nil {
		return LoginUser400JSONResponse{badRequest(apierr.Validation("тело запроса обязательно", nil))}, nil
	}

	user, err := h.users.GetByEmail(ctx, string(body.Email))
	if errors.Is(err, store.ErrUserNotFound) {
		return LoginUser401JSONResponse{unauthorized(invalidCredentials())}, nil
	}
	if err != nil {
		return nil, err
	}
	if !auth.CheckPassword(user.PasswordHash, body.Password) {
		return LoginUser401JSONResponse{unauthorized(invalidCredentials())}, nil
	}

	pair, err := h.tokens.Generate(user.ID, user.Role)
	if err != nil {
		return nil, err
	}
	return LoginUser200JSONResponse(authResponse(user, pair)), nil
}

func (h *APIHandler) RefreshToken(ctx context.Context, request RefreshTokenRequestObject) (RefreshTokenResponseObject, error) {
	body := request.Body
	if body == nil {
		return RefreshToken400JSONResponse{badRequest(apierr.Validation("тело запроса обязательно", nil))}, nil
	}

	claims, err := h.tokens.ParseRefresh(body.RefreshToken)
	if err != nil {
		return RefreshToken401JSONResponse{unauthorized(refreshTokenInvalid())}, nil
	}

	pair, err := h.tokens.Generate(claims.Subject, claims.Role)
	if err != nil {
		return nil, err
	}
	return RefreshToken200JSONResponse(tokenPair(pair)), nil
}

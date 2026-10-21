// Package apierr содержит каталог машиночитаемых кодов ошибок API
// и единый формат ошибки (контракт п.5 задания).
package apierr

import "net/http"

// Code — машиночитаемый код ошибки.
type Code string

// Каталог кодов ошибок из задания (п.5).
const (
	ValidationError         Code = "VALIDATION_ERROR"          // 400
	ProductNotFound         Code = "PRODUCT_NOT_FOUND"         // 404
	ProductInactive         Code = "PRODUCT_INACTIVE"          // 409
	OrderNotFound           Code = "ORDER_NOT_FOUND"           // 404
	OrderLimitExceeded      Code = "ORDER_LIMIT_EXCEEDED"      // 429
	OrderHasActive          Code = "ORDER_HAS_ACTIVE"          // 409
	InvalidStateTransition  Code = "INVALID_STATE_TRANSITION"  // 409
	InsufficientStock       Code = "INSUFFICIENT_STOCK"        // 409
	PromoCodeInvalid        Code = "PROMO_CODE_INVALID"        // 422
	PromoCodeMinAmount      Code = "PROMO_CODE_MIN_AMOUNT"     // 422
	OrderOwnershipViolation Code = "ORDER_OWNERSHIP_VIOLATION" // 403
)

// Дополнительные коды, которые API возвращает вне каталога заказов
// (авторизация — п.9, роли — п.10, служебные ответы).
const (
	AccessDenied        Code = "ACCESS_DENIED"         // 403
	TokenExpired        Code = "TOKEN_EXPIRED"         // 401
	TokenInvalid        Code = "TOKEN_INVALID"         // 401
	RefreshTokenInvalid Code = "REFRESH_TOKEN_INVALID" // 401
	InvalidCredentials  Code = "INVALID_CREDENTIALS"   // 401
	EmailAlreadyExists  Code = "EMAIL_ALREADY_EXISTS"  // 409
	PromoCodeExists     Code = "PROMO_CODE_EXISTS"     // 409
	InternalError       Code = "INTERNAL_ERROR"        // 500
	NotFound            Code = "NOT_FOUND"             // 404 (неизвестный маршрут)
)

// Error — единый формат ошибки API:
// error_code + message + опциональные details.
type Error struct {
	Code    Code
	Message string
	Details map[string]interface{}
}

func (e *Error) Error() string {
	return string(e.Code) + ": " + e.Message
}

// New создаёт ошибку с заданным кодом и сообщением.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithDetails добавляет/заменяет details и возвращает ошибку (fluent-стиль).
func (e *Error) WithDetails(details map[string]interface{}) *Error {
	e.Details = details
	return e
}

// WithFieldDetails добавляет details вида "поле": "нарушение".
func (e *Error) WithFieldDetails(field, violation string) *Error {
	if e.Details == nil {
		e.Details = map[string]interface{}{}
	}
	e.Details[field] = violation
	return e
}

// Validation создаёт VALIDATION_ERROR.
func Validation(message string, details map[string]interface{}) *Error {
	return New(ValidationError, message).WithDetails(details)
}

// HTTPStatus возвращает HTTP-статус, закреплённый за кодом ошибки.
// Таблица соответствия — из задания (п.5).
func (e *Error) HTTPStatus() int {
	switch e.Code {
	case ValidationError:
		return http.StatusBadRequest // 400
	case TokenExpired, TokenInvalid, RefreshTokenInvalid, InvalidCredentials:
		return http.StatusUnauthorized // 401
	case OrderOwnershipViolation, AccessDenied:
		return http.StatusForbidden // 403
	case ProductNotFound, OrderNotFound, NotFound:
		return http.StatusNotFound // 404
	case ProductInactive, OrderHasActive, InvalidStateTransition, InsufficientStock, EmailAlreadyExists,
		PromoCodeExists:
		return http.StatusConflict // 409
	case PromoCodeInvalid, PromoCodeMinAmount:
		return http.StatusUnprocessableEntity // 422
	case OrderLimitExceeded:
		return http.StatusTooManyRequests // 429
	default:
		return http.StatusInternalServerError // 500
	}
}

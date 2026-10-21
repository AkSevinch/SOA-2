package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/seva/marketplace/internal/apierr"
)

// apiError конвертирует доменную ошибку в сгенерированную модель Error
// (единый формат ответа — контракт п.5).
func apiError(e *apierr.Error) Error {
	out := Error{
		ErrorCode: ErrorCode(e.Code),
		Message:   e.Message,
	}
	if e.Details != nil {
		details := e.Details
		out.Details = &details
	}
	return out
}

// WriteError пишет ошибку в едином формате с HTTP-статусом,
// закреплённым за кодом ошибки.
func WriteError(w http.ResponseWriter, e *apierr.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.HTTPStatus())
	_ = json.NewEncoder(w).Encode(apiError(e))
}

// Хелперы для сгенерированных типов ответов с $ref на components/responses.
func badRequest(e *apierr.Error) BadRequestJSONResponse {
	return BadRequestJSONResponse(apiError(e))
}

func productNotFound(e *apierr.Error) ProductNotFoundJSONResponse {
	return ProductNotFoundJSONResponse(apiError(e))
}

func orderNotFound(e *apierr.Error) OrderNotFoundJSONResponse {
	return OrderNotFoundJSONResponse(apiError(e))
}

func orderOrProductNotFound(e *apierr.Error) OrderOrProductNotFoundJSONResponse {
	return OrderOrProductNotFoundJSONResponse(apiError(e))
}

func forbidden(e *apierr.Error) ForbiddenJSONResponse {
	return ForbiddenJSONResponse(apiError(e))
}

func orderConflict(e *apierr.Error) OrderConflictJSONResponse {
	return OrderConflictJSONResponse(apiError(e))
}

func unprocessableEntity(e *apierr.Error) UnprocessableEntityJSONResponse {
	return UnprocessableEntityJSONResponse(apiError(e))
}

func tooManyRequests(e *apierr.Error) TooManyRequestsJSONResponse {
	return TooManyRequestsJSONResponse(apiError(e))
}

func unauthorized(e *apierr.Error) UnauthorizedJSONResponse {
	return UnauthorizedJSONResponse(apiError(e))
}

func emailAlreadyExists(e *apierr.Error) EmailAlreadyExistsJSONResponse {
	return EmailAlreadyExistsJSONResponse(apiError(e))
}

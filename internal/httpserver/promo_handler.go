package httpserver

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/seva/marketplace/internal/apierr"
	"github.com/seva/marketplace/internal/store"
)

func toPromoCodeResponse(p *store.PromoCode) PromoCodeResponse {
	return PromoCodeResponse{
		Id:             uuid.MustParse(p.ID),
		Code:           p.Code,
		DiscountType:   DiscountType(p.DiscountType),
		DiscountValue:  p.DiscountValue,
		MinOrderAmount: p.MinOrderAmount,
		MaxUses:        p.MaxUses,
		CurrentUses:    p.CurrentUses,
		ValidFrom:      p.ValidFrom,
		ValidUntil:     p.ValidUntil,
		Active:         p.Active,
	}
}

func (h *APIHandler) CreatePromoCode(ctx context.Context, request CreatePromoCodeRequestObject) (CreatePromoCodeResponseObject, error) {
	if err := requireRole(ctx, "SELLER", "ADMIN"); err != nil {
		return CreatePromoCode403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	if request.Body == nil {
		return CreatePromoCode400JSONResponse{BadRequestJSONResponse: badRequest(
			apierr.Validation("Тело запроса обязательно", nil),
		)}, nil
	}

	body := request.Body
	details := map[string]interface{}{}
	if body.DiscountType == PERCENTAGE && body.DiscountValue > 100 {
		details["discount_value"] = "для PERCENTAGE значение не должно превышать 100"
	}
	if !body.ValidUntil.After(time.Now()) {
		details["valid_until"] = "должно быть в будущем"
	}
	if len(details) > 0 {
		return CreatePromoCode400JSONResponse{BadRequestJSONResponse: badRequest(
			apierr.Validation("Ошибка валидации входных данных", details),
		)}, nil
	}

	promo, err := h.promos.Create(ctx, store.PromoCodeParams{
		Code:           body.Code,
		DiscountType:   string(body.DiscountType),
		DiscountValue:  body.DiscountValue,
		MinOrderAmount: body.MinOrderAmount,
		MaxUses:        body.MaxUses,
		ValidUntil:     body.ValidUntil,
	})
	if errors.Is(err, store.ErrPromoCodeExists) {
		return CreatePromoCode409JSONResponse{PromoCodeConflictJSONResponse: PromoCodeConflictJSONResponse(
			apiError(apierr.New(apierr.PromoCodeExists, "Промокод с таким кодом уже существует")),
		)}, nil
	}
	if err != nil {
		return nil, err
	}
	return CreatePromoCode201JSONResponse(toPromoCodeResponse(promo)), nil
}

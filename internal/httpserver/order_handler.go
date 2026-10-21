package httpserver

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/seva/marketplace/internal/apierr"
	"github.com/seva/marketplace/internal/store"
)

func toOrderResponse(o *store.Order) OrderResponse {
	items := make([]OrderItemResponse, 0, len(o.Items))
	for _, item := range o.Items {
		items = append(items, OrderItemResponse{
			ProductId:    uuid.MustParse(item.ProductID),
			Quantity:     item.Quantity,
			PriceAtOrder: item.PriceAtOrder,
		})
	}
	return OrderResponse{
		Id:             uuid.MustParse(o.ID),
		UserId:         uuid.MustParse(o.UserID),
		Status:         OrderStatus(o.Status),
		PromoCode:      o.PromoCode,
		Items:          items,
		TotalAmount:    o.TotalAmount,
		DiscountAmount: o.DiscountAmount,
		CreatedAt:      o.CreatedAt,
		UpdatedAt:      o.UpdatedAt,
	}
}

func toOrderItemInputs(items []OrderItemInput) []store.OrderItemInput {
	out := make([]store.OrderItemInput, 0, len(items))
	for _, item := range items {
		out = append(out, store.OrderItemInput{
			ProductID: item.ProductId.String(),
			Quantity:  item.Quantity,
		})
	}
	return out
}

func orderNotFoundError() *apierr.Error {
	return apierr.New(apierr.OrderNotFound, "Заказ не найден")
}

// mapOrderError переводит доменные ошибки заказов в единый формат (п.5).
// nil означает неожиданную ошибку — она уйдёт в 500.
func mapOrderError(err error) *apierr.Error {
	var (
		stockErr   *store.InsufficientStockError
		productErr *store.ProductError
	)
	switch {
	case errors.As(err, &stockErr):
		items := make([]interface{}, 0, len(stockErr.Shortages))
		for _, shortage := range stockErr.Shortages {
			items = append(items, map[string]interface{}{
				"product_id": shortage.ProductID,
				"requested":  shortage.Requested,
				"available":  shortage.Available,
			})
		}
		return apierr.New(apierr.InsufficientStock, "Недостаточно товара на складе").
			WithDetails(map[string]interface{}{"items": items})
	case errors.As(err, &productErr):
		if productErr.Inactive {
			return apierr.New(apierr.ProductInactive, "Товар неактивен").
				WithFieldDetails("product_id", productErr.ProductID)
		}
		return apierr.New(apierr.ProductNotFound, "Товар не найден").
			WithFieldDetails("product_id", productErr.ProductID)
	case errors.Is(err, store.ErrOrderHasActive):
		return apierr.New(apierr.OrderHasActive, "У пользователя уже есть активный заказ")
	case errors.Is(err, store.ErrOrderRateLimited):
		return apierr.New(apierr.OrderLimitExceeded, "Превышен лимит частоты операций")
	case errors.Is(err, store.ErrInvalidStateTransition):
		return apierr.New(apierr.InvalidStateTransition, "Недопустимый переход состояния заказа")
	case errors.Is(err, store.ErrPromoCodeInvalid):
		return apierr.New(apierr.PromoCodeInvalid, "Промокод не найден, истёк, исчерпан или неактивен")
	case errors.Is(err, store.ErrPromoCodeMinAmount):
		return apierr.New(apierr.PromoCodeMinAmount, "Сумма заказа ниже минимальной для промокода")
	case errors.Is(err, store.ErrOrderNotFound):
		return orderNotFoundError()
	case errors.Is(err, store.ErrOwnershipViolation):
		return apierr.New(apierr.OrderOwnershipViolation, "Заказ принадлежит другому пользователю")
	}
	return nil
}

func (h *APIHandler) CreateOrder(ctx context.Context, request CreateOrderRequestObject) (CreateOrderResponseObject, error) {
	if err := requireRole(ctx, "USER", "ADMIN"); err != nil {
		return CreateOrder403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	if request.Body == nil {
		return CreateOrder400JSONResponse{BadRequestJSONResponse: badRequest(
			apierr.Validation("Тело запроса обязательно", nil),
		)}, nil
	}
	order, err := h.orders.Create(ctx, store.CreateOrderParams{
		UserID:    UserIDFromContext(ctx),
		Items:     toOrderItemInputs(request.Body.Items),
		PromoCode: request.Body.PromoCode,
	})
	if err != nil {
		return createOrderErrorResponse(err)
	}
	return CreateOrder201JSONResponse(toOrderResponse(order)), nil
}

func createOrderErrorResponse(err error) (CreateOrderResponseObject, error) {
	apiErr := mapOrderError(err)
	if apiErr == nil {
		return nil, err
	}
	switch apiErr.Code {
	case apierr.ProductNotFound:
		return CreateOrder404JSONResponse{ProductNotFoundJSONResponse: productNotFound(apiErr)}, nil
	case apierr.ProductInactive, apierr.OrderHasActive, apierr.InsufficientStock:
		return CreateOrder409JSONResponse{OrderConflictJSONResponse: orderConflict(apiErr)}, nil
	case apierr.PromoCodeInvalid, apierr.PromoCodeMinAmount:
		return CreateOrder422JSONResponse{UnprocessableEntityJSONResponse: unprocessableEntity(apiErr)}, nil
	case apierr.OrderLimitExceeded:
		return CreateOrder429JSONResponse{TooManyRequestsJSONResponse: tooManyRequests(apiErr)}, nil
	}
	return nil, err
}

func (h *APIHandler) GetOrder(ctx context.Context, request GetOrderRequestObject) (GetOrderResponseObject, error) {
	if err := requireRole(ctx, "USER", "ADMIN"); err != nil {
		return GetOrder403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	order, err := h.orders.Get(ctx, UserIDFromContext(ctx), request.Id.String(), isAdmin(ctx))
	if err != nil {
		apiErr := mapOrderError(err)
		if apiErr == nil {
			return nil, err
		}
		switch apiErr.Code {
		case apierr.OrderNotFound:
			return GetOrder404JSONResponse{OrderNotFoundJSONResponse: orderNotFound(apiErr)}, nil
		case apierr.OrderOwnershipViolation:
			return GetOrder403JSONResponse{ForbiddenJSONResponse: forbidden(apiErr)}, nil
		}
		return nil, err
	}
	return GetOrder200JSONResponse(toOrderResponse(order)), nil
}

func (h *APIHandler) UpdateOrder(ctx context.Context, request UpdateOrderRequestObject) (UpdateOrderResponseObject, error) {
	if err := requireRole(ctx, "USER", "ADMIN"); err != nil {
		return UpdateOrder403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	if request.Body == nil {
		return UpdateOrder400JSONResponse{BadRequestJSONResponse: badRequest(
			apierr.Validation("Тело запроса обязательно", nil),
		)}, nil
	}
	order, err := h.orders.Update(ctx, UserIDFromContext(ctx), request.Id.String(),
		toOrderItemInputs(request.Body.Items), isAdmin(ctx))
	if err != nil {
		apiErr := mapOrderError(err)
		if apiErr == nil {
			return nil, err
		}
		switch apiErr.Code {
		case apierr.OrderNotFound, apierr.ProductNotFound:
			return UpdateOrder404JSONResponse{OrderOrProductNotFoundJSONResponse: orderOrProductNotFound(apiErr)}, nil
		case apierr.OrderOwnershipViolation:
			return UpdateOrder403JSONResponse{ForbiddenJSONResponse: forbidden(apiErr)}, nil
		case apierr.ProductInactive, apierr.InvalidStateTransition, apierr.InsufficientStock:
			return UpdateOrder409JSONResponse{OrderConflictJSONResponse: orderConflict(apiErr)}, nil
		case apierr.OrderLimitExceeded:
			return UpdateOrder429JSONResponse{TooManyRequestsJSONResponse: tooManyRequests(apiErr)}, nil
		}
		return nil, err
	}
	return UpdateOrder200JSONResponse(toOrderResponse(order)), nil
}

func (h *APIHandler) CancelOrder(ctx context.Context, request CancelOrderRequestObject) (CancelOrderResponseObject, error) {
	if err := requireRole(ctx, "USER", "ADMIN"); err != nil {
		return CancelOrder403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	order, err := h.orders.Cancel(ctx, UserIDFromContext(ctx), request.Id.String(), isAdmin(ctx))
	if err != nil {
		apiErr := mapOrderError(err)
		if apiErr == nil {
			return nil, err
		}
		switch apiErr.Code {
		case apierr.OrderNotFound:
			return CancelOrder404JSONResponse{OrderNotFoundJSONResponse: orderNotFound(apiErr)}, nil
		case apierr.OrderOwnershipViolation:
			return CancelOrder403JSONResponse{ForbiddenJSONResponse: forbidden(apiErr)}, nil
		case apierr.InvalidStateTransition:
			return CancelOrder409JSONResponse{OrderConflictJSONResponse: orderConflict(apiErr)}, nil
		}
		return nil, err
	}
	return CancelOrder200JSONResponse(toOrderResponse(order)), nil
}

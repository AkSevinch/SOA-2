package httpserver

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/seva/marketplace/internal/apierr"
	"github.com/seva/marketplace/internal/auth"
	"github.com/seva/marketplace/internal/store"
)

const (
	defaultPage = int32(0)
	defaultSize = int32(20)
	maxPageSize = int32(100)
)

// APIHandler реализует сгенерированный StrictServerInterface — здесь живёт бизнес-логика.
type APIHandler struct {
	products *store.ProductStore
	orders   *store.OrderStore
	users    *store.UserStore
	promos   *store.PromoCodeStore
	tokens   *auth.TokenService
}

func NewHandler(products *store.ProductStore, orders *store.OrderStore, users *store.UserStore,
	promos *store.PromoCodeStore, tokens *auth.TokenService) *APIHandler {
	return &APIHandler{products: products, orders: orders, users: users, promos: promos, tokens: tokens}
}

func ensureProductOwnership(ctx context.Context, p *store.Product) *apierr.Error {
	if isAdmin(ctx) {
		return nil
	}
	if p.SellerID == nil || *p.SellerID != UserIDFromContext(ctx) {
		return accessDenied()
	}
	return nil
}

var _ StrictServerInterface = (*APIHandler)(nil)

func productNotFoundError() *apierr.Error {
	return apierr.New(apierr.ProductNotFound, "Товар не найден")
}

func toResponse(p *store.Product) ProductResponse {
	return ProductResponse{
		Id:          uuid.MustParse(p.ID),
		Name:        p.Name,
		Description: p.Description,
		Price:       p.Price,
		Stock:       p.Stock,
		Category:    p.Category,
		Status:      ProductStatus(p.Status),
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func toStoreParams(body *ProductCreate) store.ProductParams {
	return store.ProductParams{
		Name:        body.Name,
		Description: body.Description,
		Price:       body.Price,
		Stock:       body.Stock,
		Category:    body.Category,
		Status:      string(body.Status),
	}
}

func toStoreParamsUpdate(body *ProductUpdate) store.ProductParams {
	return toStoreParams(&ProductCreate{
		Name:        body.Name,
		Description: body.Description,
		Price:       body.Price,
		Stock:       body.Stock,
		Category:    body.Category,
		Status:      body.Status,
	})
}

func (h *APIHandler) CreateProduct(ctx context.Context, request CreateProductRequestObject) (CreateProductResponseObject, error) {
	if err := requireRole(ctx, "SELLER", "ADMIN"); err != nil {
		return CreateProduct403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	if request.Body == nil {
		return CreateProduct400JSONResponse{BadRequestJSONResponse: badRequest(
			apierr.Validation("Тело запроса обязательно", nil),
		)}, nil
	}
	params := toStoreParams(request.Body)
	if !isAdmin(ctx) {
		sellerID := UserIDFromContext(ctx)
		params.SellerID = &sellerID
	}
	p, err := h.products.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	return CreateProduct201JSONResponse(toResponse(p)), nil
}

func (h *APIHandler) GetProduct(ctx context.Context, request GetProductRequestObject) (GetProductResponseObject, error) {
	p, err := h.products.GetByID(ctx, request.Id.String())
	if errors.Is(err, store.ErrNotFound) {
		return GetProduct404JSONResponse{ProductNotFoundJSONResponse: productNotFound(productNotFoundError())}, nil
	}
	if err != nil {
		return nil, err
	}
	return GetProduct200JSONResponse(toResponse(p)), nil
}

func (h *APIHandler) ListProducts(ctx context.Context, request ListProductsRequestObject) (ListProductsResponseObject, error) {
	page := defaultPage
	if request.Params.Page != nil {
		page = *request.Params.Page
	}
	size := defaultSize
	if request.Params.Size != nil {
		size = *request.Params.Size
	}

	var status *string
	if request.Params.Status != nil {
		s := string(*request.Params.Status)
		status = &s
	}

	items, total, err := h.products.List(ctx, store.ListFilter{
		Status:   status,
		Category: request.Params.Category,
		Page:     page,
		Size:     size,
	})
	if err != nil {
		return nil, err
	}

	response := make([]ProductResponse, 0, len(items))
	for i := range items {
		response = append(response, toResponse(&items[i]))
	}
	return ListProducts200JSONResponse(ProductPage{
		Items:         response,
		TotalElements: total,
		Page:          page,
		Size:          size,
	}), nil
}

func (h *APIHandler) UpdateProduct(ctx context.Context, request UpdateProductRequestObject) (UpdateProductResponseObject, error) {
	if err := requireRole(ctx, "SELLER", "ADMIN"); err != nil {
		return UpdateProduct403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	if request.Body == nil {
		return UpdateProduct400JSONResponse{BadRequestJSONResponse: badRequest(
			apierr.Validation("Тело запроса обязательно", nil),
		)}, nil
	}
	existing, err := h.products.GetByID(ctx, request.Id.String())
	if errors.Is(err, store.ErrNotFound) {
		return UpdateProduct404JSONResponse{ProductNotFoundJSONResponse: productNotFound(productNotFoundError())}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := ensureProductOwnership(ctx, existing); err != nil {
		return UpdateProduct403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	p, err := h.products.Update(ctx, request.Id.String(), toStoreParamsUpdate(request.Body))
	if errors.Is(err, store.ErrNotFound) {
		return UpdateProduct404JSONResponse{ProductNotFoundJSONResponse: productNotFound(productNotFoundError())}, nil
	}
	if err != nil {
		return nil, err
	}
	return UpdateProduct200JSONResponse(toResponse(p)), nil
}

func (h *APIHandler) DeleteProduct(ctx context.Context, request DeleteProductRequestObject) (DeleteProductResponseObject, error) {
	if err := requireRole(ctx, "SELLER", "ADMIN"); err != nil {
		return DeleteProduct403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	existing, err := h.products.GetByID(ctx, request.Id.String())
	if errors.Is(err, store.ErrNotFound) {
		return DeleteProduct404JSONResponse{ProductNotFoundJSONResponse: productNotFound(productNotFoundError())}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := ensureProductOwnership(ctx, existing); err != nil {
		return DeleteProduct403JSONResponse{ForbiddenJSONResponse: forbidden(err)}, nil
	}
	err = h.products.SoftDelete(ctx, request.Id.String())
	if errors.Is(err, store.ErrNotFound) {
		return DeleteProduct404JSONResponse{ProductNotFoundJSONResponse: productNotFound(productNotFoundError())}, nil
	}
	if err != nil {
		return nil, err
	}
	return DeleteProduct204Response{}, nil
}

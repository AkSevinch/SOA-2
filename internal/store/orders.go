package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Доменные ошибки заказов (п.7). Хендлеры маппят их в apierr.
var (
	ErrOrderNotFound          = errors.New("order not found")
	ErrOwnershipViolation     = errors.New("order ownership violation")
	ErrOrderHasActive         = errors.New("user has active order")
	ErrOrderRateLimited       = errors.New("order rate limit exceeded")
	ErrProductNotFound        = errors.New("product not found")
	ErrProductInactive        = errors.New("product inactive")
	ErrInsufficientStock      = errors.New("insufficient stock")
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrPromoCodeInvalid       = errors.New("promo code invalid")
	ErrPromoCodeMinAmount     = errors.New("promo code min amount")
)

// ProductError указывает, какой товар нарушил проверку каталога.
type ProductError struct {
	ProductID string
	Inactive  bool
}

func (e *ProductError) Error() string {
	if e.Inactive {
		return fmt.Sprintf("product %s is inactive", e.ProductID)
	}
	return fmt.Sprintf("product %s not found", e.ProductID)
}

func (e *ProductError) Unwrap() error {
	if e.Inactive {
		return ErrProductInactive
	}
	return ErrProductNotFound
}

// StockShortage описывает нехватку остатка по конкретному товару.
type StockShortage struct {
	ProductID string
	Requested int32
	Available int32
}

// InsufficientStockError содержит список товаров с недостаточным остатком.
type InsufficientStockError struct {
	Shortages []StockShortage
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("insufficient stock for %d product(s)", len(e.Shortages))
}

func (e *InsufficientStockError) Unwrap() error {
	return ErrInsufficientStock
}

type OrderItemInput struct {
	ProductID string
	Quantity  int32
}

type OrderItem struct {
	ProductID    string
	Quantity     int32
	PriceAtOrder float64
}

type Order struct {
	ID             string
	UserID         string
	Status         string
	PromoCodeID    *string
	PromoCode      *string
	TotalAmount    float64
	DiscountAmount float64
	Items          []OrderItem
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateOrderParams struct {
	UserID    string
	Items     []OrderItemInput
	PromoCode *string
}

type OrderStore struct {
	pool      *pgxpool.Pool
	rateLimit time.Duration
}

// NewOrderStore создаёт хранилище заказов.
// rateLimit — минимальный интервал между операциями одного типа для пользователя;
// 0 отключает ограничение.
func NewOrderStore(pool *pgxpool.Pool, rateLimit time.Duration) *OrderStore {
	return &OrderStore{pool: pool, rateLimit: rateLimit}
}

const (
	opCreateOrder = "CREATE_ORDER"
	opUpdateOrder = "UPDATE_ORDER"
)

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Create создаёт заказ: все проверки и записи — в одной транзакции.
func (s *OrderStore) Create(ctx context.Context, params CreateOrderParams) (*Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create order: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.checkRateLimit(ctx, tx, params.UserID, opCreateOrder); err != nil {
		return nil, err
	}

	var hasActive bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM orders
			WHERE user_id = $1::uuid AND status IN ('CREATED', 'PAYMENT_PENDING')
		)`, params.UserID).Scan(&hasActive); err != nil {
		return nil, fmt.Errorf("check active order: %w", err)
	}
	if hasActive {
		return nil, ErrOrderHasActive
	}

	items, err := s.prepareItems(ctx, tx, params.Items)
	if err != nil {
		return nil, err
	}

	total := itemsTotal(items)
	var (
		discount float64
		promoID  *string
	)
	if params.PromoCode != nil {
		discount, promoID, err = s.applyPromoCode(ctx, tx, *params.PromoCode, total)
		if err != nil {
			return nil, err
		}
	}

	var orderID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO orders (user_id, status, promo_code_id, total_amount, discount_amount)
		VALUES ($1::uuid, 'CREATED', $2::uuid, $3, $4)
		RETURNING id::text`,
		params.UserID, promoID, round2(total-discount), discount,
	).Scan(&orderID); err != nil {
		return nil, fmt.Errorf("insert order: %w", err)
	}

	if err := s.insertItems(ctx, tx, orderID, items); err != nil {
		return nil, err
	}
	if err := s.reserveStock(ctx, tx, items); err != nil {
		return nil, err
	}
	if err := s.recordOperation(ctx, tx, params.UserID, opCreateOrder); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create order: %w", err)
	}
	return s.Get(ctx, params.UserID, orderID, false)
}

// Get возвращает заказ вместе с позициями. admin обходит проверку владельца.
func (s *OrderStore) Get(ctx context.Context, userID, orderID string, admin bool) (*Order, error) {
	order, err := s.loadOrder(ctx, s.pool, orderID)
	if err != nil {
		return nil, err
	}
	if !admin && order.UserID != userID {
		return nil, ErrOwnershipViolation
	}
	items, err := s.loadItems(ctx, s.pool, orderID)
	if err != nil {
		return nil, err
	}
	order.Items = items
	return order, nil
}

// Update заменяет позиции заказа: возврат старых остатков, резерв новых,
// пересчёт стоимости и промокода — в одной транзакции.
func (s *OrderStore) Update(ctx context.Context, userID, orderID string, items []OrderItemInput, admin bool) (*Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin update order: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := s.lockOrder(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if !admin && order.UserID != userID {
		return nil, ErrOwnershipViolation
	}
	if order.Status != "CREATED" {
		return nil, ErrInvalidStateTransition
	}
	if err := s.checkRateLimit(ctx, tx, userID, opUpdateOrder); err != nil {
		return nil, err
	}

	oldItems, err := s.loadItems(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if err := s.releaseStock(ctx, tx, oldItems); err != nil {
		return nil, err
	}

	newItems, err := s.prepareItems(ctx, tx, items)
	if err != nil {
		return nil, err
	}

	total := itemsTotal(newItems)
	discount := order.DiscountAmount
	promoID := order.PromoCodeID
	if promoID != nil {
		discount, promoID, err = s.recalculatePromoCode(ctx, tx, *promoID, total)
		if err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE orders
		SET promo_code_id   = $2::uuid,
		    total_amount    = $3,
		    discount_amount = $4
		WHERE id = $1::uuid`,
		orderID, promoID, round2(total-discount), discount,
	); err != nil {
		return nil, fmt.Errorf("update order: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM order_items WHERE order_id = $1::uuid`, orderID); err != nil {
		return nil, fmt.Errorf("delete order items: %w", err)
	}
	if err := s.insertItems(ctx, tx, orderID, newItems); err != nil {
		return nil, err
	}
	if err := s.reserveStock(ctx, tx, newItems); err != nil {
		return nil, err
	}
	if err := s.recordOperation(ctx, tx, userID, opUpdateOrder); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit update order: %w", err)
	}
	return s.Get(ctx, userID, orderID, admin)
}

// Cancel отменяет заказ: возврат остатков и использования промокода
// в одной транзакции.
func (s *OrderStore) Cancel(ctx context.Context, userID, orderID string, admin bool) (*Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cancel order: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := s.lockOrder(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if !admin && order.UserID != userID {
		return nil, ErrOwnershipViolation
	}
	if order.Status != "CREATED" && order.Status != "PAYMENT_PENDING" {
		return nil, ErrInvalidStateTransition
	}

	items, err := s.loadItems(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if err := s.releaseStock(ctx, tx, items); err != nil {
		return nil, err
	}
	if order.PromoCodeID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE promo_codes
			SET current_uses = GREATEST(current_uses - 1, 0)
			WHERE id = $1::uuid`, *order.PromoCodeID); err != nil {
			return nil, fmt.Errorf("release promo code: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE orders SET status = 'CANCELED' WHERE id = $1::uuid`, orderID); err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cancel order: %w", err)
	}
	return s.Get(ctx, userID, orderID, admin)
}

// prepareItems объединяет дубликаты, блокирует товары и проверяет каталог/остатки.
func (s *OrderStore) prepareItems(ctx context.Context, tx pgx.Tx, inputs []OrderItemInput) ([]OrderItem, error) {
	ids, quantities := mergeItems(inputs)
	items := make([]OrderItem, 0, len(ids))
	shortages := make([]StockShortage, 0)

	for _, id := range ids {
		var (
			price  float64
			stock  int32
			status string
		)
		err := tx.QueryRow(ctx, `
			SELECT price::float8, stock, status::text
			FROM products
			WHERE id = $1::uuid
			FOR UPDATE`, id).Scan(&price, &stock, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &ProductError{ProductID: id}
		}
		if err != nil {
			return nil, fmt.Errorf("lock product %s: %w", id, err)
		}
		if status != "ACTIVE" {
			return nil, &ProductError{ProductID: id, Inactive: true}
		}
		quantity := quantities[id]
		if stock < quantity {
			shortages = append(shortages, StockShortage{
				ProductID: id,
				Requested: quantity,
				Available: stock,
			})
			continue
		}
		items = append(items, OrderItem{
			ProductID:    id,
			Quantity:     quantity,
			PriceAtOrder: price,
		})
	}
	if len(shortages) > 0 {
		return nil, &InsufficientStockError{Shortages: shortages}
	}
	return items, nil
}

func mergeItems(inputs []OrderItemInput) ([]string, map[string]int32) {
	ids := make([]string, 0, len(inputs))
	quantities := make(map[string]int32, len(inputs))
	for _, item := range inputs {
		if _, ok := quantities[item.ProductID]; !ok {
			ids = append(ids, item.ProductID)
		}
		quantities[item.ProductID] += item.Quantity
	}
	return ids, quantities
}

func (s *OrderStore) insertItems(ctx context.Context, tx pgx.Tx, orderID string, items []OrderItem) error {
	for _, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_items (order_id, product_id, quantity, price_at_order)
			VALUES ($1::uuid, $2::uuid, $3, $4)`,
			orderID, item.ProductID, item.Quantity, item.PriceAtOrder,
		); err != nil {
			return fmt.Errorf("insert order item: %w", err)
		}
	}
	return nil
}

func (s *OrderStore) reserveStock(ctx context.Context, tx pgx.Tx, items []OrderItem) error {
	for _, item := range items {
		if _, err := tx.Exec(ctx, `
			UPDATE products SET stock = stock - $1 WHERE id = $2::uuid`,
			item.Quantity, item.ProductID,
		); err != nil {
			return fmt.Errorf("reserve stock for %s: %w", item.ProductID, err)
		}
	}
	return nil
}

func (s *OrderStore) releaseStock(ctx context.Context, tx pgx.Tx, items []OrderItem) error {
	for _, item := range items {
		if _, err := tx.Exec(ctx, `
			UPDATE products SET stock = stock + $1 WHERE id = $2::uuid`,
			item.Quantity, item.ProductID,
		); err != nil {
			return fmt.Errorf("release stock for %s: %w", item.ProductID, err)
		}
	}
	return nil
}

func (s *OrderStore) loadOrder(ctx context.Context, q querier, orderID string) (*Order, error) {
	var order Order
	err := q.QueryRow(ctx, `
		SELECT o.id::text, o.user_id::text, o.status::text, o.promo_code_id::text,
		       p.code, o.total_amount::float8, o.discount_amount::float8,
		       o.created_at, o.updated_at
		FROM orders o
		LEFT JOIN promo_codes p ON p.id = o.promo_code_id
		WHERE o.id = $1::uuid`, orderID,
	).Scan(
		&order.ID, &order.UserID, &order.Status, &order.PromoCodeID,
		&order.PromoCode, &order.TotalAmount, &order.DiscountAmount,
		&order.CreatedAt, &order.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load order: %w", err)
	}
	return &order, nil
}

func (s *OrderStore) lockOrder(ctx context.Context, tx pgx.Tx, orderID string) (*Order, error) {
	var order Order
	err := tx.QueryRow(ctx, `
		SELECT id::text, user_id::text, status::text, promo_code_id::text,
		       total_amount::float8, discount_amount::float8
		FROM orders
		WHERE id = $1::uuid
		FOR UPDATE`, orderID,
	).Scan(
		&order.ID, &order.UserID, &order.Status, &order.PromoCodeID,
		&order.TotalAmount, &order.DiscountAmount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock order: %w", err)
	}
	return &order, nil
}

func (s *OrderStore) loadItems(ctx context.Context, q querier, orderID string) ([]OrderItem, error) {
	rows, err := q.Query(ctx, `
		SELECT product_id::text, quantity, price_at_order::float8
		FROM order_items
		WHERE order_id = $1::uuid
		ORDER BY id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("load order items: %w", err)
	}
	defer rows.Close()

	items := make([]OrderItem, 0)
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(&item.ProductID, &item.Quantity, &item.PriceAtOrder); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *OrderStore) checkRateLimit(ctx context.Context, tx pgx.Tx, userID, operation string) error {
	if s.rateLimit <= 0 {
		return nil
	}
	var last time.Time
	err := tx.QueryRow(ctx, `
		SELECT created_at
		FROM user_operations
		WHERE user_id = $1::uuid AND operation_type = $2::user_operation_type
		ORDER BY created_at DESC
		LIMIT 1`, userID, operation).Scan(&last)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check rate limit: %w", err)
	}
	if time.Since(last) < s.rateLimit {
		return ErrOrderRateLimited
	}
	return nil
}

func (s *OrderStore) recordOperation(ctx context.Context, tx pgx.Tx, userID, operation string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_operations (user_id, operation_type)
		VALUES ($1::uuid, $2::user_operation_type)`, userID, operation); err != nil {
		return fmt.Errorf("record user operation: %w", err)
	}
	return nil
}

type promoCode struct {
	ID             string
	Type           string
	Value          float64
	MinOrderAmount float64
	MaxUses        int32
	CurrentUses    int32
	ValidFrom      time.Time
	ValidUntil     time.Time
	Active         bool
}

// applyPromoCode проверяет промокод, считает скидку и увеличивает счётчик использований.
func (s *OrderStore) applyPromoCode(ctx context.Context, tx pgx.Tx, code string, total float64) (float64, *string, error) {
	promo, err := s.loadPromoCode(ctx, tx, code)
	if errors.Is(err, ErrPromoCodeInvalid) {
		return 0, nil, err
	}
	if err != nil {
		return 0, nil, err
	}
	now := time.Now()
	if !promo.Active || promo.CurrentUses >= promo.MaxUses ||
		now.Before(promo.ValidFrom) || now.After(promo.ValidUntil) {
		return 0, nil, ErrPromoCodeInvalid
	}
	if total < promo.MinOrderAmount {
		return 0, nil, ErrPromoCodeMinAmount
	}
	if _, err := tx.Exec(ctx, `
		UPDATE promo_codes SET current_uses = current_uses + 1 WHERE id = $1::uuid`,
		promo.ID); err != nil {
		return 0, nil, fmt.Errorf("increment promo code uses: %w", err)
	}
	return promoDiscount(promo, total), &promo.ID, nil
}

// recalculatePromoCode перепроверяет условия промокода при обновлении заказа.
// Если промокод больше не применим — снимает его и возвращает использование.
func (s *OrderStore) recalculatePromoCode(ctx context.Context, tx pgx.Tx, promoID string, total float64) (float64, *string, error) {
	promo, err := s.loadPromoCodeByID(ctx, tx, promoID)
	if err != nil {
		return 0, nil, err
	}
	now := time.Now()
	applicable := promo.Active &&
		!now.Before(promo.ValidFrom) && !now.After(promo.ValidUntil) &&
		total >= promo.MinOrderAmount
	if !applicable {
		if _, err := tx.Exec(ctx, `
			UPDATE promo_codes
			SET current_uses = GREATEST(current_uses - 1, 0)
			WHERE id = $1::uuid`, promoID); err != nil {
			return 0, nil, fmt.Errorf("release promo code: %w", err)
		}
		return 0, nil, nil
	}
	return promoDiscount(promo, total), &promo.ID, nil
}

func (s *OrderStore) loadPromoCode(ctx context.Context, tx pgx.Tx, code string) (*promoCode, error) {
	return scanPromoCode(tx.QueryRow(ctx, `
		SELECT id::text, discount_type::text, discount_value::float8,
		       min_order_amount::float8, max_uses, current_uses,
		       valid_from, valid_until, active
		FROM promo_codes
		WHERE code = $1
		FOR UPDATE`, code))
}

func (s *OrderStore) loadPromoCodeByID(ctx context.Context, tx pgx.Tx, id string) (*promoCode, error) {
	return scanPromoCode(tx.QueryRow(ctx, `
		SELECT id::text, discount_type::text, discount_value::float8,
		       min_order_amount::float8, max_uses, current_uses,
		       valid_from, valid_until, active
		FROM promo_codes
		WHERE id = $1::uuid
		FOR UPDATE`, id))
}

func scanPromoCode(row pgx.Row) (*promoCode, error) {
	var promo promoCode
	err := row.Scan(
		&promo.ID, &promo.Type, &promo.Value,
		&promo.MinOrderAmount, &promo.MaxUses, &promo.CurrentUses,
		&promo.ValidFrom, &promo.ValidUntil, &promo.Active,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPromoCodeInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("load promo code: %w", err)
	}
	return &promo, nil
}

// promoDiscount считает скидку: для PERCENTAGE — не более 70% суммы,
// для FIXED_AMOUNT — не более суммы заказа.
func promoDiscount(promo *promoCode, total float64) float64 {
	var discount float64
	switch promo.Type {
	case "PERCENTAGE":
		discount = total * promo.Value / 100
		if maxDiscount := total * 0.7; discount > maxDiscount {
			discount = maxDiscount
		}
	case "FIXED_AMOUNT":
		discount = math.Min(promo.Value, total)
	}
	return round2(discount)
}

func itemsTotal(items []OrderItem) float64 {
	var total float64
	for _, item := range items {
		total += item.PriceAtOrder * float64(item.Quantity)
	}
	return round2(total)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

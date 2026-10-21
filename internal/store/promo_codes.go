package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPromoCodeExists = errors.New("promo code already exists")

type PromoCode struct {
	ID             string
	Code           string
	DiscountType   string
	DiscountValue  float64
	MinOrderAmount float64
	MaxUses        int32
	CurrentUses    int32
	ValidFrom      time.Time
	ValidUntil     time.Time
	Active         bool
}

type PromoCodeParams struct {
	Code           string
	DiscountType   string
	DiscountValue  float64
	MinOrderAmount float64
	MaxUses        int32
	ValidUntil     time.Time
}

const promoCodeColumns = `
	id::text, code, discount_type::text, discount_value::float8, min_order_amount::float8,
	max_uses, current_uses, valid_from, valid_until, active`

type PromoCodeStore struct {
	pool *pgxpool.Pool
}

func NewPromoCodeStore(pool *pgxpool.Pool) *PromoCodeStore {
	return &PromoCodeStore{pool: pool}
}

func (s *PromoCodeStore) Create(ctx context.Context, params PromoCodeParams) (*PromoCode, error) {
	var p PromoCode
	err := s.pool.QueryRow(ctx, `
		INSERT INTO promo_codes (code, discount_type, discount_value, min_order_amount, max_uses, valid_until)
		VALUES ($1, $2::discount_type, $3, $4, $5, $6)
		RETURNING `+promoCodeColumns,
		params.Code, params.DiscountType, params.DiscountValue, params.MinOrderAmount,
		params.MaxUses, params.ValidUntil,
	).Scan(&p.ID, &p.Code, &p.DiscountType, &p.DiscountValue, &p.MinOrderAmount,
		&p.MaxUses, &p.CurrentUses, &p.ValidFrom, &p.ValidUntil, &p.Active)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrPromoCodeExists
	}
	if err != nil {
		return nil, fmt.Errorf("create promo code: %w", err)
	}
	return &p, nil
}

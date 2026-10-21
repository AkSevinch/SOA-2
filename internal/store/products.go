package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound возвращается, когда товар не найден по id.
var ErrNotFound = errors.New("product not found")

type Product struct {
	ID          string
	Name        string
	Description *string
	Price       float64
	Stock       int32
	Category    string
	Status      string
	SellerID    *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ListFilter struct {
	Status   *string
	Category *string
	Page     int32
	Size     int32
}

type ProductParams struct {
	Name        string
	Description *string
	Price       float64
	Stock       int32
	Category    string
	Status      string
	SellerID    *string
}

const productColumns = `
	id::text, name, description, price::float8, stock, category,
	status::text, seller_id::text, created_at, updated_at`

type ProductStore struct {
	pool *pgxpool.Pool
}

func NewProductStore(pool *pgxpool.Pool) *ProductStore {
	return &ProductStore{pool: pool}
}

func scanProduct(row pgx.Row) (*Product, error) {
	var p Product
	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.Category,
		&p.Status, &p.SellerID, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan product: %w", err)
	}
	return &p, nil
}

func (s *ProductStore) Create(ctx context.Context, params ProductParams) (*Product, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO products (name, description, price, stock, category, status, seller_id)
		VALUES ($1, $2, $3, $4, $5, $6::product_status, $7::uuid)
		RETURNING `+productColumns,
		params.Name, params.Description, params.Price, params.Stock, params.Category, params.Status,
		params.SellerID,
	)
	return scanProduct(row)
}

func (s *ProductStore) GetByID(ctx context.Context, id string) (*Product, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+productColumns+`
		FROM products
		WHERE id = $1::uuid`, id)
	return scanProduct(row)
}

func (s *ProductStore) List(ctx context.Context, f ListFilter) ([]Product, int64, error) {
	where := `
		WHERE ($1::product_status IS NULL OR status = $1::product_status)
		  AND ($2::text IS NULL OR category = $2)`

	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM products`+where, f.Status, f.Category).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+productColumns+`
		FROM products`+where+`
		ORDER BY created_at, id
		LIMIT $3 OFFSET $4`,
		f.Status, f.Category, f.Size, int64(f.Page)*int64(f.Size),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	items := make([]Product, 0)
	for rows.Next() {
		var p Product
		err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.Category,
			&p.Status, &p.SellerID, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan product: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *ProductStore) Update(ctx context.Context, id string, params ProductParams) (*Product, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE products
		SET name        = $2,
		    description = $3,
		    price       = $4,
		    stock       = $5,
		    category    = $6,
		    status      = $7::product_status
		WHERE id = $1::uuid
		RETURNING `+productColumns,
		id, params.Name, params.Description, params.Price, params.Stock, params.Category, params.Status,
	)
	return scanProduct(row)
}

// SoftDelete переводит товар в статус ARCHIVED (мягкое удаление).
func (s *ProductStore) SoftDelete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE products
		SET status = 'ARCHIVED'
		WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("archive product: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

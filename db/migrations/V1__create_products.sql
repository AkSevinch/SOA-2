-- П.4: таблица products, индекс по status, автоматические created_at/updated_at

CREATE TYPE product_status AS ENUM ('ACTIVE', 'INACTIVE', 'ARCHIVED');

CREATE TABLE products
(
    id          UUID PRIMARY KEY         DEFAULT gen_random_uuid(),
    name        VARCHAR(255)    NOT NULL,
    description VARCHAR(4000),
    price       DECIMAL(12, 2)  NOT NULL CHECK (price > 0),
    stock       INTEGER         NOT NULL CHECK (stock >= 0),
    category    VARCHAR(100)    NOT NULL,
    status      product_status  NOT NULL DEFAULT 'ACTIVE',
    seller_id   UUID,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT now()
);

-- Требование п.4: индекс на поле status
CREATE INDEX idx_products_status ON products (status);

-- created_at/updated_at заполняются автоматически
CREATE OR REPLACE FUNCTION set_updated_at()
    RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_products_updated_at
    BEFORE UPDATE
    ON products
    FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

-- П.7: заказы, позиции заказов, промокоды и операции пользователя

CREATE TYPE order_status AS ENUM (
    'CREATED',
    'PAYMENT_PENDING',
    'PAID',
    'SHIPPED',
    'COMPLETED',
    'CANCELED'
);

CREATE TYPE discount_type AS ENUM ('PERCENTAGE', 'FIXED_AMOUNT');

CREATE TYPE user_operation_type AS ENUM ('CREATE_ORDER', 'UPDATE_ORDER');

CREATE TABLE promo_codes
(
    id               UUID PRIMARY KEY         DEFAULT gen_random_uuid(),
    code             VARCHAR(20)      NOT NULL UNIQUE,
    discount_type    discount_type    NOT NULL,
    discount_value   DECIMAL(12, 2)   NOT NULL CHECK (discount_value >= 0),
    min_order_amount DECIMAL(12, 2)   NOT NULL DEFAULT 0 CHECK (min_order_amount >= 0),
    max_uses         INTEGER          NOT NULL CHECK (max_uses >= 0),
    current_uses     INTEGER          NOT NULL DEFAULT 0 CHECK (current_uses >= 0),
    valid_from       TIMESTAMPTZ      NOT NULL DEFAULT now(),
    valid_until      TIMESTAMPTZ      NOT NULL,
    active           BOOLEAN          NOT NULL DEFAULT true,
    CONSTRAINT promo_codes_uses_check CHECK (current_uses <= max_uses),
    CONSTRAINT promo_codes_period_check CHECK (valid_until > valid_from)
);

CREATE TABLE orders
(
    id              UUID PRIMARY KEY        DEFAULT gen_random_uuid(),
    user_id         UUID           NOT NULL,
    status          order_status   NOT NULL DEFAULT 'CREATED',
    promo_code_id   UUID           REFERENCES promo_codes (id),
    total_amount    DECIMAL(12, 2) NOT NULL CHECK (total_amount >= 0),
    discount_amount DECIMAL(12, 2) NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now()
);

-- Активные заказы пользователя и выборка по владельцу
CREATE INDEX idx_orders_user_status ON orders (user_id, status);

CREATE TRIGGER trg_orders_updated_at
    BEFORE UPDATE
    ON orders
    FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TABLE order_items
(
    id             UUID PRIMARY KEY       DEFAULT gen_random_uuid(),
    order_id       UUID           NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    product_id     UUID           NOT NULL REFERENCES products (id),
    quantity       INTEGER        NOT NULL CHECK (quantity > 0),
    price_at_order DECIMAL(12, 2) NOT NULL CHECK (price_at_order > 0)
);

CREATE INDEX idx_order_items_order ON order_items (order_id);

CREATE TABLE user_operations
(
    id             UUID PRIMARY KEY       DEFAULT gen_random_uuid(),
    user_id        UUID           NOT NULL,
    operation_type user_operation_type NOT NULL,
    created_at     TIMESTAMPTZ    NOT NULL DEFAULT now()
);

-- Rate-limit: последняя операция пользователя нужного типа
CREATE INDEX idx_user_operations_rate
    ON user_operations (user_id, operation_type, created_at DESC);

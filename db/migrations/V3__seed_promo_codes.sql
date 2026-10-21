-- П.7: тестовые промокоды (эндпоинт создания промокодов появится в п.10)

INSERT INTO promo_codes (id, code, discount_type, discount_value, min_order_amount,
                         max_uses, current_uses, valid_from, valid_until, active)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'SAVE10', 'PERCENTAGE', 10, 0,
     100, 0, now() - INTERVAL '1 day', now() + INTERVAL '365 days', true),
    ('22222222-2222-2222-2222-222222222222', 'FIXED100', 'FIXED_AMOUNT', 100, 500,
     100, 0, now() - INTERVAL '1 day', now() + INTERVAL '365 days', true),
    ('33333333-3333-3333-3333-333333333333', 'VIP80', 'PERCENTAGE', 80, 1000,
     100, 0, now() - INTERVAL '1 day', now() + INTERVAL '365 days', true),
    ('44444444-4444-4444-4444-444444444444', 'EXPIRED', 'PERCENTAGE', 10, 0,
     100, 0, now() - INTERVAL '10 days', now() - INTERVAL '1 day', true),
    ('55555555-5555-5555-5555-555555555555', 'LIMITED', 'PERCENTAGE', 5, 0,
     1, 0, now() - INTERVAL '1 day', now() + INTERVAL '365 days', true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO users (id, email, password_hash, role)
VALUES ('11111111-1111-4111-8111-111111111111', 'user@example.com',
        '$2a$10$nO/IHTaA2FNKFxn67Utu/OT6co/EuQ29rmnzfBnbn09Hf77Zhk6d.', 'USER'),
       ('22222222-2222-4222-8222-222222222222', 'seller@example.com',
        '$2a$10$K0Pr.uIxG7Ny0o/gIy5DqulLY2tLwBsh5tRDDu6UrRMrFaRqhF9ve', 'SELLER'),
       ('33333333-3333-4333-8333-333333333333', 'admin@example.com',
        '$2a$10$q2vvYK/5DRCKb4tPsK4Rv.m4u42H4HwwHKFf13Exr7yiP9GZqOeua', 'ADMIN')
ON CONFLICT (email) DO NOTHING;

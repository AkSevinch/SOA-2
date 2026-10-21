# Marketplace API

Домашнее задание №2 «Marketplace: OpenAPI + CRUD». Контракт-first сервис: OpenAPI 3.0.3 → сгенерированный Go-код, CRUD товаров, заказы с резервированием остатков и промокодами, JWT-авторизация и ролевая модель `USER` / `SELLER` / `ADMIN`.

## Стек

| Слой | Технология |
|---|---|
| Язык | Go 1.26 |
| HTTP | chi v5 + oapi-codegen (chi-server, strict-server) |
| Контракт | OpenAPI 3.0.3, oapi-codegen v2.8.0 |
| Валидация | kin-openapi + oapi-codegen/nethttp-middleware |
| БД | PostgreSQL 17, pgx v5, Flyway |
| Авторизация | JWT HS256 (golang-jwt/jwt/v5), bcrypt (golang.org/x/crypto) |
| Логи | `log/slog` (JSON) |
| Инфраструктура | Docker Compose: `db`, `flyway`, `app`, `swagger-ui` |

## Быстрый старт

```bash
docker compose up -d --build      # db + миграции + app + swagger-ui
docker compose ps
```

- API: <http://localhost:8080>
- Swagger UI: <http://localhost:8081/swagger/>

Полный сброс (пересоздать БД и образ):

```bash
docker compose down -v && make up
```

Локально без Docker (нужна PostgreSQL и применённые миграции):

```bash
make generate
make fmt && make vet && go test ./...
make run
```

Миграции применяются **только Flyway** (`db/migrations/V1..V5`), вручную их не выполняют.

## Команды

| Команда | Действие |
|---|---|
| `make generate` | кодогенерация из `api/openapi.yaml` (`go generate ./...`) |
| `make build` | `generate` + `go build -o bin/server ./cmd/server` |
| `make run` | `build` + запуск сервера |
| `make up` / `make down` | `docker compose up --build -d` / `down -v` |
| `make logs` | логи всех сервисов |
| `make fmt` / `make vet` | `gofmt -w .` / `go vet ./...` |

## Структура

```
api/openapi.yaml        # спецификация — источник истины
cmd/server/main.go      # точка входа, DI, middleware-цепочка
internal/httpserver/    # сгенерированный код, хендлеры, middleware, RBAC
internal/store/         # доступ к БД (pgx): products, orders, promo_codes, users
internal/auth/          # JWT и bcrypt
internal/apierr/        # каталог кодов ошибок и единый формат Error
db/migrations/          # Flyway V1..V5
docker-compose.yaml     # db, flyway, app, swagger-ui
```

## Реализовано по пунктам задания

| Пункт | Что сделано | Где |
|---|---|---|
| 1. OpenAPI CRUD | 5 эндпоинтов `/products`, пагинация `page`/`size` (с 0), фильтры `status`/`category` | `api/openapi.yaml` |
| 2. Схемы данных | `ProductCreate` / `ProductUpdate` / `ProductResponse`, `format`, `enum`, `nullable`, `required` | `api/openapi.yaml` |
| 3. Кодогенерация | типы и роутер генерируются из спеки, ручных DTO нет | `internal/httpserver/cfg.yaml`, `generate.go` |
| 4. PostgreSQL + CRUD | миграции `V1`, soft-delete → `ARCHIVED`, пагинация и фильтрация в SQL | `db/migrations`, `internal/store/products.go` |
| 5. Контракт ошибок | единый `Error{error_code, message, details}`, каталог кодов, маппинг код→HTTP | `internal/apierr`, `internal/httpserver/errors.go` |
| 6. Валидация | kin-openapi отклоняет невалидные запросы до бизнес-логики → `400 VALIDATION_ERROR` | `internal/httpserver/validator.go` |
| 7. Логика заказов | транзакции, `FOR UPDATE`, резерв/возврат остатков, снапшот цен, промокоды, rate-limit, статусы | `internal/store/orders.go`, `order_handler.go` |
| 8. Логирование | JSON-логи, `request_id`, `user_id`, `duration_ms`, маскирование секретов | `internal/httpserver/logging.go` |
| 9. Авторизация | register/login/refresh, access 20 мин + refresh 14 дней, защита всех CRUD | `internal/auth`, `auth_handler.go`, `auth_middleware.go` |
| 10. Роли | `role` в JWT, `requireRole`, владелец `seller_id`, admin-bypass | `internal/httpserver/rbac.go`, `handler.go` |

## Архитектурные решения

- **Спецификация — источник истины**: код и типы генерируются из `api/openapi.yaml`, ручных DTO нет.
- **Валидация до бизнес-логики**: middleware kin-openapi возвращает `400` раньше хендлера.
- **Единый контракт ошибок**: любой ответ об ошибке — `error_code` + `message` (+ `details`).
- **Транзакции и блокировки**: резервирование остатков через `SELECT ... FOR UPDATE`, снапшот цен в `order_items`.
- **Rate-limit заказов**: таблица `user_operations` + окно `ORDER_RATE_LIMIT_MINUTES` (0 — отключено).
- **Промокоды**: `PERCENTAGE` / `FIXED_AMOUNT`, лимиты использований и минимальной суммы, скидка не более 70% суммы.
- **JWT**: claim `role` и `typ` (`access` / `refresh`), refresh self-contained (без таблицы сессий).
- **RBAC**: проверка роли + владельца товара (`seller_id`), `ADMIN` обходит проверку владельца.
- **Логи**: `slog` JSON; `request_id` — новый UUID на каждый запрос, тело логируется только для `POST/PUT/PATCH/DELETE`, `password` маскируется.


## API

| Метод | Endpoint | Доступ |
|---|---|---|
| `POST` | `/auth/register` | публично → `USER` |
| `POST` | `/auth/login` | публично |
| `POST` | `/auth/refresh` | публично (нужен refresh token) |
| `GET` | `/products` | все роли |
| `GET` | `/products/{id}` | все роли |
| `POST` | `/products` | `SELLER`, `ADMIN` |
| `PUT` | `/products/{id}` | `SELLER` (свой), `ADMIN` |
| `DELETE` | `/products/{id}` | `SELLER` (свой), `ADMIN` (soft-delete) |
| `POST` | `/orders` | `USER`, `ADMIN` |
| `GET` | `/orders/{id}` | `USER` (свой), `ADMIN` |
| `PUT` | `/orders/{id}` | `USER` (свой), `ADMIN` |
| `POST` | `/orders/{id}/cancel` | `USER` (свой), `ADMIN` |
| `POST` | `/promo-codes` | `SELLER`, `ADMIN` |

## Коды ошибок

| Код | HTTP | Код | HTTP |
|---|---|---|---|
| `VALIDATION_ERROR` | 400 | `ACCESS_DENIED` | 403 |
| `TOKEN_INVALID` | 401 | `TOKEN_EXPIRED` | 401 |
| `REFRESH_TOKEN_INVALID` | 401 | `INVALID_CREDENTIALS` | 401 |
| `PRODUCT_NOT_FOUND` | 404 | `ORDER_NOT_FOUND` | 404 |
| `NOT_FOUND` | 404 | `PRODUCT_INACTIVE` | 409 |
| `ORDER_HAS_ACTIVE` | 409 | `INVALID_STATE_TRANSITION` | 409 |
| `INSUFFICIENT_STOCK` | 409 | `EMAIL_ALREADY_EXISTS` | 409 |
| `PROMO_CODE_EXISTS` | 409 | `ORDER_OWNERSHIP_VIOLATION` | 403 |
| `PROMO_CODE_INVALID` | 422 | `PROMO_CODE_MIN_AMOUNT` | 422 |
| `ORDER_LIMIT_EXCEEDED` | 429 | `INTERNAL_ERROR` | 500 |

## Демо-аккаунты

| Роль | Email | Пароль |
|---|---|---|
| `USER` | `user@example.com` | `user12345` |
| `SELLER` | `seller@example.com` | `seller12345` |
| `ADMIN` | `admin@example.com` | `admin12345` |

Логин:

```bash
curl -s -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"seller12345"}'
```

В Swagger UI: `Authorize` → вставить **только** `access_token` (без слова `Bearer`, UI добавит его сам).

## Тесты и проверка БД

```bash
go test ./...    # 25 тестов: apierr, JWT, middleware, логирование, CORS, RBAC
```

```bash
docker compose exec -T db psql -U marketplace -d marketplace \
  -c "SELECT version, description, success FROM flyway_schema_history ORDER BY installed_rank"

docker compose exec -T db psql -U marketplace -d marketplace \
  -c "SELECT id, name, price, stock, status, seller_id FROM products ORDER BY created_at DESC LIMIT 5"
```

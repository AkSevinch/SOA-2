# Сборка: кодогенерация из OpenAPI (п.3) + компиляция приложения
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Генерация кода из api/openapi.yaml — одна команда, воспроизводимо при сборке
RUN go generate ./... && CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM alpine:3.22
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]

package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	openapimw "github.com/oapi-codegen/nethttp-middleware"

	"github.com/seva/marketplace/internal/apierr"
)

// NewRequestValidator создаёт middleware, валидирующий входящие запросы
// по OpenAPI-спецификации — единственному источнику истины (контракт п.6).
// Невалидный запрос отклоняется до бизнес-логики и БД.
func NewRequestValidator() (func(http.Handler) http.Handler, error) {
	spec, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("загрузка OpenAPI-спецификации: %w", err)
	}
	// Спека описывает публичный адрес, а сервис может работать за прокси
	// или на другом хосте — проверку заголовка Host отключаем.
	spec.Servers = nil

	return openapimw.OapiRequestValidatorWithOptions(spec, &openapimw.Options{
		Options: openapi3filter.Options{
			MultiError: true, // собираем все нарушения, а не только первое
			// Авторизацию проверяет AuthMiddleware, валидатор не должен требовать токен.
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		ErrorHandlerWithOpts: handleValidationError,
	}), nil
}

// handleValidationError приводит ошибки kin-openapi к единому формату (п.5).
func handleValidationError(_ context.Context, err error, w http.ResponseWriter, _ *http.Request, opts openapimw.ErrorHandlerOpts) {
	if opts.MatchedRoute == nil {
		WriteError(w, apierr.New(apierr.NotFound, "эндпоинт не найден"))
		return
	}

	details := map[string]interface{}{}
	collectViolations(err, details)
	if len(details) == 0 {
		details = nil
	}
	WriteError(w, apierr.Validation("Ошибка валидации входных данных", details))
}

// collectViolations разворачивает цепочку ошибок kin-openapi
// в плоскую карту «поле → нарушение».
func collectViolations(err error, details map[string]interface{}) {
	switch e := err.(type) {
	case openapi3.MultiError:
		for _, child := range e {
			collectViolations(child, details)
		}
	case *openapi3filter.RequestError:
		field := "body"
		if e.Parameter != nil {
			field = e.Parameter.Name
		}
		switch child := e.Err.(type) {
		case nil:
			addDetail(details, field, violationText(e))
		case *openapi3.SchemaError:
			addSchemaViolation(details, field, child)
		case openapi3.MultiError:
			for _, item := range child {
				var se *openapi3.SchemaError
				if errors.As(item, &se) {
					addSchemaViolation(details, field, se)
					continue
				}
				addDetail(details, field, item.Error())
			}
		default:
			if e.RequestBody != nil && e.Reason == "failed to decode request body" {
				addDetail(details, "body", "некорректный JSON")
				return
			}
			addDetail(details, field, violationText(e))
		}
	case *openapi3.SchemaError:
		addSchemaViolation(details, "body", e)
	default:
		addDetail(details, "body", err.Error())
	}
}

func addSchemaViolation(details map[string]interface{}, fallback string, se *openapi3.SchemaError) {
	field := fallback
	if ptr := se.JSONPointer(); len(ptr) > 0 {
		field = strings.Join(ptr, ".")
	}
	addDetail(details, field, schemaViolation(se))
}

func addDetail(details map[string]interface{}, field, violation string) {
	if prev, ok := details[field].(string); ok && prev != "" {
		details[field] = prev + "; " + violation
		return
	}
	details[field] = violation
}

// schemaViolation переводит нарушение JSON-схемы в человекочитаемый текст.
func schemaViolation(se *openapi3.SchemaError) string {
	s := se.Schema
	switch se.SchemaField {
	case "minLength":
		return fmt.Sprintf("длина должна быть не меньше %d", s.MinLength)
	case "maxLength":
		if s.MaxLength != nil {
			return fmt.Sprintf("длина не должна превышать %d", *s.MaxLength)
		}
	case "minimum":
		if s.Min != nil {
			return "значение должно быть >= " + formatNumber(*s.Min)
		}
	case "maximum":
		if s.Max != nil {
			return "значение должно быть <= " + formatNumber(*s.Max)
		}
	case "enum":
		return "допустимы значения: " + joinEnum(s.Enum)
	case "required":
		return "обязательное поле"
	case "pattern":
		return "значение не соответствует шаблону " + s.Pattern
	case "type":
		if s.Type != nil {
			return fmt.Sprintf("ожидается тип %s", s.Type)
		}
	}
	return se.Reason
}

func violationText(e *openapi3filter.RequestError) string {
	switch {
	case errors.Is(e.Err, openapi3filter.ErrInvalidRequired):
		return "обязательное значение"
	case errors.Is(e.Err, openapi3filter.ErrInvalidEmptyValue):
		return "пустое значение недопустимо"
	case e.Reason != "":
		return e.Reason
	case e.Err != nil:
		return e.Err.Error()
	}
	return "некорректное значение"
}

func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func joinEnum(values []interface{}) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, fmt.Sprint(v))
	}
	return strings.Join(parts, ", ")
}

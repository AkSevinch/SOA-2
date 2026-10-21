package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("лог не является JSON: %v\n%s", err, buf.String())
	}
	return entry
}

func TestRequestLoggerPOSTLogsBodyAndRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	const requestBody = `{"password":"secret","nested":{"token":"abc"},"name":"ok"}`
	var handlerBody string
	h := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("чтение тела в хендлере: %v", err)
		}
		handlerBody = string(raw)
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/orders?x=1", strings.NewReader(requestBody))
	req.Header.Set("X-Request-Id", "fixed-by-client")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if handlerBody != requestBody {
		t.Fatalf("тело не восстановлено после логгера: %q", handlerBody)
	}

	responseID := rec.Header().Get("X-Request-Id")
	if responseID == "" {
		t.Fatal("в ответе нет X-Request-Id")
	}
	if responseID == "fixed-by-client" {
		t.Fatal("request_id должен генерироваться заново, а не браться из запроса")
	}

	entry := decodeLog(t, &buf)
	if entry["request_id"] != responseID {
		t.Fatalf("request_id в логе (%v) не совпадает с X-Request-Id (%s)", entry["request_id"], responseID)
	}
	if entry["method"] != http.MethodPost {
		t.Fatalf("method = %v", entry["method"])
	}
	if entry["endpoint"] != "/orders" {
		t.Fatalf("endpoint = %v", entry["endpoint"])
	}
	if entry["status_code"] != float64(http.StatusCreated) {
		t.Fatalf("status_code = %v", entry["status_code"])
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatal("нет duration_ms")
	}
	if v, ok := entry["user_id"]; !ok || v != nil {
		t.Fatalf("user_id = %v (ожидался null)", v)
	}
	if _, err := time.Parse(time.RFC3339, entry["timestamp"].(string)); err != nil {
		t.Fatalf("timestamp не RFC3339: %v", err)
	}

	body, ok := entry["request_body"].(map[string]any)
	if !ok {
		t.Fatalf("request_body = %#v", entry["request_body"])
	}
	if body["password"] != "***" {
		t.Fatalf("password не замаскирован: %v", body["password"])
	}
	nested, ok := body["nested"].(map[string]any)
	if !ok || nested["token"] != "***" {
		t.Fatalf("nested.token не замаскирован: %#v", body["nested"])
	}
	if body["name"] != "ok" {
		t.Fatalf("name = %v", body["name"])
	}
}

func TestRequestLoggerGETDoesNotLogBody(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	}))
	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	entry := decodeLog(t, &buf)
	if entry["status_code"] != float64(http.StatusOK) {
		t.Fatalf("status_code = %v", entry["status_code"])
	}
	if _, ok := entry["request_body"]; ok {
		t.Fatal("для GET тело логироваться не должно")
	}
}

func TestRequestLoggerLogsErrorStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	req := httptest.NewRequest(http.MethodDelete, "/orders/1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	entry := decodeLog(t, &buf)
	if entry["status_code"] != float64(http.StatusConflict) {
		t.Fatalf("status_code = %v", entry["status_code"])
	}
}

func TestStatusRecorderDefaultStatus(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if _, err := rec.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if rec.status != http.StatusOK {
		t.Fatalf("status = %d", rec.status)
	}
}

func TestMaskSensitiveNonJSON(t *testing.T) {
	got := loggableBody([]byte("not json"))
	if got != "not json" {
		t.Fatalf("got = %#v", got)
	}
}

func TestRequestLoggerIncludesUserID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetUserID(r.Context(), "user-42")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/products", nil))

	entry := decodeLog(t, &buf)
	if entry["user_id"] != "user-42" {
		t.Fatalf("user_id = %v", entry["user_id"])
	}
}

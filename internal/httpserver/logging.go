package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type ctxKeyUserID struct{}
type ctxKeyUserIDHolder struct{}

type userIDHolder struct {
	id string
}

func withUserIDHolder(ctx context.Context) (context.Context, *userIDHolder) {
	holder := &userIDHolder{}
	return context.WithValue(ctx, ctxKeyUserIDHolder{}, holder), holder
}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxKeyUserID{}, userID)
}

func SetUserID(ctx context.Context, userID string) context.Context {
	if holder, ok := ctx.Value(ctxKeyUserIDHolder{}).(*userIDHolder); ok {
		holder.id = userID
	}
	return WithUserID(ctx, userID)
}

func UserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if holder, ok := ctx.Value(ctxKeyUserIDHolder{}).(*userIDHolder); ok && holder.id != "" {
		return holder.id
	}
	id, _ := ctx.Value(ctxKeyUserID{}).(string)
	return id
}

var sensitiveKeys = map[string]struct{}{
	"password":      {},
	"password_hash": {},
	"passwordhash":  {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"secret":        {},
}

func maskSensitive(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if _, ok := sensitiveKeys[strings.ToLower(k)]; ok {
				t[k] = "***"
				continue
			}
			t[k] = maskSensitive(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = maskSensitive(t[i])
		}
		return t
	default:
		return v
	}
}

func loggableBody(body []byte) any {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	return maskSensitive(v)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := uuid.NewString()
			ctx := context.WithValue(r.Context(), middleware.RequestIDKey, requestID)
			ctx, _ = withUserIDHolder(ctx)
			r = r.WithContext(ctx)
			w.Header().Set(middleware.RequestIDHeader, requestID)

			var body any
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				raw, err := io.ReadAll(r.Body)
				if err == nil {
					body = loggableBody(raw)
					r.Body = io.NopCloser(bytes.NewReader(raw))
					r.GetBody = func() (io.ReadCloser, error) {
						return io.NopCloser(bytes.NewReader(raw)), nil
					}
				}
			}

			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			userID := UserIDFromContext(ctx)
			var userAttr slog.Attr
			if userID == "" {
				userAttr = slog.Any("user_id", nil)
			} else {
				userAttr = slog.String("user_id", userID)
			}

			attrs := []slog.Attr{
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("endpoint", r.URL.Path),
				slog.Int("status_code", status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				userAttr,
				slog.String("timestamp", time.Now().UTC().Format(time.RFC3339)),
			}
			if body != nil {
				attrs = append(attrs, slog.Any("request_body", body))
			}
			logger.LogAttrs(ctx, slog.LevelInfo, "request", attrs...)
		})
	}
}

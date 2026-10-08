package service

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"github.com/superdb/super/api"
	"github.com/superdb/super/service/srverr"
	"go.uber.org/zap"
)

// requestIDMiddleware adds the unique identifier of the request to the request
// context. If the header "X-Request-ID" exists this will be used, otherwise
// one will be generated.
func requestIDMiddleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := r.Header.Get(api.RequestIDHeader)
			if reqID == "" {
				reqID = uuid.NewV7().String()
			}
			w.Header().Add(api.RequestIDHeader, reqID)
			ctx := context.WithValue(r.Context(), api.RequestIDHeader, reqID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func corsMiddleware(allowedOrigins []string) mux.MiddlewareFunc {
	return cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{
			http.MethodHead,
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
		},
		AllowedHeaders:   []string{"Authorization", "Accept", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
	}).Handler
}

func accessLogMiddleware(logger *zap.Logger) mux.MiddlewareFunc {
	logger = logger.Named("http.access")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := logger.With(zap.String("request_id", api.RequestIDFromContext(r.Context())))
			detailedLogger := logger.With(
				zap.String("host", r.Host),
				zap.String("method", r.Method),
				zap.String("proto", r.Proto),
				zap.String("remote_addr", r.RemoteAddr),
				zap.Int64("request_content_length", r.ContentLength),
				zap.Stringer("url", r.URL),
			)
			recorder := newRecordingResponseWriter(w)
			w = recorder
			detailedLogger.Debug("Request started")
			defer func(start time.Time) {
				detailedLogger.Info("Request completed",
					zap.Duration("elapsed", time.Since(start)),
					zap.Int("response_content_length", recorder.contentLength),
					zap.Int("status_code", recorder.statusCode),
				)
			}(time.Now())
			next.ServeHTTP(w, r)
		})
	}
}

func panicCatchMiddleware(logger *zap.Logger) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				logger.DPanic("Panic",
					zap.Error(srverr.RecoverError(rec)),
					zap.String("request_id", api.RequestIDFromContext(r.Context())),
					zap.Stack("stack"),
				)
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// recordingResponseWriter wraps an http.ResponseWriter to record the content
// length and status code of the response.
type recordingResponseWriter struct {
	http.ResponseWriter
	contentLength int
	statusCode    int
}

func newRecordingResponseWriter(w http.ResponseWriter) *recordingResponseWriter {
	return &recordingResponseWriter{
		ResponseWriter: w,
		statusCode:     200, // Default status code is 200.
	}
}

func (r *recordingResponseWriter) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *recordingResponseWriter) Write(data []byte) (int, error) {
	r.contentLength += len(data)
	return r.ResponseWriter.Write(data)
}

func (r *recordingResponseWriter) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

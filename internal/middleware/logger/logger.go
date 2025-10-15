package logger

import (
	"log"
	"net/http"
	"time"

	"github.com/urfave/negroni/v3"
)

func LoggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			timeNow := time.Now()
			rw := negroni.NewResponseWriter(w)
			next.ServeHTTP(rw, r)
			elapsed := time.Since(timeNow)
			log.Printf(
				"[http_logger] %s | %3d | %9s | %s | %s %s",
				timeNow.Format(time.RFC3339),
				rw.Status(),
				elapsed,
				r.Host,
				r.Method,
				r.URL.Path,
			)
		})
	}
}

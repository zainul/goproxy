package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
)

// PanicRecovery middleware recovers from panics and logs stack trace
func PanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("Panic recovered: %v\nStack trace:\n%s", err, debug.Stack())
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
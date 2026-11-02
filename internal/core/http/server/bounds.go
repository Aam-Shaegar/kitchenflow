package core_http_server

import (
	request "github.com/Aam-Shaegar/kitchenflow/internal/core/http/request"
	"net/http"
	"time"
)

// ApplyBounds is shared by both entry points. ReadTimeout covers body reads too.
func ApplyBounds(s *http.Server) {
	s.ReadHeaderTimeout = 5 * time.Second
	s.ReadTimeout = 15 * time.Second
	s.WriteTimeout = 30 * time.Second
	s.IdleTimeout = 60 * time.Second
	s.MaxHeaderBytes = 32 * 1024
	s.Handler = LimitBody(s.Handler)
}
func LimitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > request.MaxBodyBytes {
			http.Error(w, "request body too large", 413)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, request.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

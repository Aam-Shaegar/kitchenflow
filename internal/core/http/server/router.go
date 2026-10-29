package core_http_server

import (
	"fmt"
	"net/http"

	core_http_middleware "github.com/Aam-Shaegar/kitchenflow/internal/core/http/middleware"
)

type ApiVersion string

var (
	ApiVersion1 = ApiVersion("v1")
)

type APIVersionRouter struct {
	*http.ServeMux
	apiVersion ApiVersion
	middleware []core_http_middleware.Middleware
}

func NewAPIVersionRouter(apiVersion ApiVersion, middleware ...core_http_middleware.Middleware) *APIVersionRouter {
	return &APIVersionRouter{
		ServeMux:   http.NewServeMux(),
		apiVersion: apiVersion,
		middleware: middleware,
	}
}

func (r *APIVersionRouter) RegisterRoutes(routes ...Route) {
	for _, route := range routes {
		pattern := fmt.Sprintf("%s %s", route.Method, route.Path)

		r.Handle(pattern, route.WithMiddleware())
	}
}

func (r *APIVersionRouter) WithMiddleware() http.Handler {
	return core_http_middleware.ChainMiddleware(
		r,
		r.middleware...,
	)
}

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	core_config "github.com/Aam-Shaegar/kitchenflow/internal/core/config"
	core_migrate "github.com/Aam-Shaegar/kitchenflow/internal/core/database/migrate"
	core_pgx_pool "github.com/Aam-Shaegar/kitchenflow/internal/core/database/pgx"
	core_http_middleware "github.com/Aam-Shaegar/kitchenflow/internal/core/http/middleware"
	core_http_server "github.com/Aam-Shaegar/kitchenflow/internal/core/http/server"
	core_logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"

	restaurants_repository "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/repository"
	restaurants_service "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/service"
	restaurants_handler "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/transport/http"

	menu_repository "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/repository"
	menu_service "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/service"
	menu_handler "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/transport/http"

	cart_repository "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	cart_service "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/service"
	cart_handler "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/transport/http"

	orders_repository "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"
	orders_service "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/service"
	orders_handler "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/transport/http"

	integrations_http "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/http"
	integrations_handler "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/transport/http"

	"go.uber.org/zap"
)

func main() {
	cfg := core_config.NewConfigMust()
	time.Local = cfg.TimeZone

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	fmt.Println("Kitchen API starting")

	logger, err := core_logger.NewLogger(core_logger.NewConfigMust())
	if err != nil {
		fmt.Println("failed to init application logger:", err)
		os.Exit(1)
	}
	defer logger.Close()

	logger.Debug("application time zone", zap.Any("zone", time.Local))

	logger.Debug("initializing postgres connection pool")
	pool, err := core_pgx_pool.NewConnectionPool(core_pgx_pool.NewConfigMust(), ctx)
	if err != nil {
		logger.Fatal("failed to init postgres connection pool", zap.Error(err))
	}
	defer pool.Close()

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
		if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
			migrationsDir = "/app/migrations"
		}
	}
	logger.Debug("running database migrations", zap.String("dir", migrationsDir))
	if err := core_migrate.Run(ctx, pool, migrationsDir); err != nil {
		logger.Fatal("failed to run migrations", zap.Error(err))
	}

	restaurantsRepo := restaurants_repository.NewRestaurantRepository(pool)
	menuRepo := menu_repository.NewMenuRepository(pool)
	cartRepo := cart_repository.NewCartRepository(pool)
	ordersRepo := orders_repository.NewOrderRepository(pool)

	restaurantsSvc := restaurants_service.NewRestaurantService(restaurantsRepo)
	menuSvc := menu_service.NewMenuService(menuRepo)
	cartSvc := cart_service.NewCartService(cartRepo, menuSvc)

	restaurantClient, err := integrations_http.NewHTTPClient(10*time.Second,
		strings.FieldsFunc(os.Getenv("RESTAURANT_ALLOWED_ORIGINS"), func(r rune) bool { return r == ',' }),
		strings.FieldsFunc(os.Getenv("RESTAURANT_ALLOWED_PRIVATE_CIDRS"), func(r rune) bool { return r == ',' }),
	)
	if err != nil {
		logger.Fatal("invalid restaurant integration configuration", zap.Error(err))
	}

	ordersSvc := orders_service.NewOrderService(
		ordersRepo,
		cartRepo,
		menuSvc,
		restaurantsSvc,
		restaurantClient,
	)

	restaurantsHandler := restaurants_handler.NewRestaurantHandler(restaurantsSvc)
	menuHandler := menu_handler.NewMenuHandler(menuSvc)
	cartHandler := cart_handler.NewCartHandler(cartSvc)
	ordersHandler := orders_handler.NewOrderHandler(ordersSvc)
	integrationHandler := integrations_handler.NewIntegrationHandler(ordersSvc)

	apiRouter := core_http_server.NewAPIVersionRouter(core_http_server.ApiVersion1)

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			logger.Error("Failed to write health response", zap.Error(err))
		}
	}

	apiRouter.RegisterRoutes(
		core_http_server.NewRoute(http.MethodGet, "/health", healthHandler),
		core_http_server.NewRoute(http.MethodGet, "/restaurants", restaurantsHandler.ListRestaurants),
		core_http_server.NewRoute(http.MethodPost, "/restaurants", restaurantsHandler.CreateRestaurant),
		core_http_server.NewRoute(http.MethodGet, "/restaurants/{restaurant_id}", restaurantsHandler.GetRestaurant),
		core_http_server.NewRoute(http.MethodPatch, "/restaurants/{restaurant_id}", restaurantsHandler.UpdateRestaurant),
		core_http_server.NewRoute(http.MethodGet, "/restaurants/{restaurant_id}/menu", menuHandler.GetMenu),
		core_http_server.NewRoute(http.MethodPost, "/restaurants/{restaurant_id}/menu/categories", menuHandler.CreateCategory),
		core_http_server.NewRoute(http.MethodPost, "/restaurants/{restaurant_id}/menu/items", menuHandler.CreateItem),
		core_http_server.NewRoute(http.MethodGet, "/restaurants/{restaurant_id}/orders", ordersHandler.ListRestaurantOrders),
		core_http_server.NewRoute(http.MethodGet, "/menu-items/{menu_item_id}", menuHandler.GetMenuItem),
		core_http_server.NewRoute(http.MethodPatch, "/menu-items/{menu_item_id}", menuHandler.UpdateItem),
		core_http_server.NewRoute(http.MethodDelete, "/menu-items/{menu_item_id}", menuHandler.DeleteItem),
		core_http_server.NewRoute(http.MethodGet, "/cart", cartHandler.GetCart,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodPost, "/cart/items", cartHandler.AddItem,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodPatch, "/cart/items/{cart_item_id}", cartHandler.UpdateItem,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodDelete, "/cart/items/{cart_item_id}", cartHandler.RemoveItem,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodDelete, "/cart", cartHandler.ClearCart,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodPost, "/orders", ordersHandler.CreateOrder,
			core_http_middleware.RequireHeader("X-User-ID"),
			core_http_middleware.RequireHeader("Idempotency-Key")),
		core_http_server.NewRoute(http.MethodGet, "/orders", ordersHandler.GetOrders,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodGet, "/orders/{order_id}", ordersHandler.GetOrder,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodGet, "/orders/{order_id}/status", ordersHandler.GetOrderStatus,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodGet, "/orders/{order_id}/history", ordersHandler.GetOrderHistory,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodPost, "/orders/{order_id}/cancel", ordersHandler.CancelOrder,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodPost, "/orders/{order_id}/retry", ordersHandler.RetryOrder,
			core_http_middleware.RequireHeader("X-User-ID")),
		core_http_server.NewRoute(http.MethodPost, "/integrations/restaurants/{restaurant_id}/orders/status", integrationHandler.RestaurantCallback),
	)

	logger.Debug("initializing http server")
	httpServer := core_http_server.NewHTTPServer(
		core_http_server.NewConfigMust(),
		logger,
		core_http_middleware.CORS("*"),
		core_http_middleware.RequestID(),
		core_http_middleware.Logger(logger),
		core_http_middleware.Trace(),
		core_http_middleware.Panic(),
	)
	httpServer.RegisterAPIRouters(apiRouter)

	if err := httpServer.Run(ctx); err != nil {
		logger.Error("HTTP server run error", zap.Error(err))
	}

	logger.Debug("shutdown complete")
}

package main

import (
	"context"
	"fmt"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/callbackauth"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/health"
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

	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	auth, err := core_http_middleware.LoadAuth(os.Getenv("AUTH_CREDENTIALS_FILE"))
	if err != nil {
		return err
	}
	callbackKeys, err := callbackauth.Load(os.Getenv("CALLBACK_KEYS_FILE"))
	if err != nil {
		return err
	}
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
		return fmt.Errorf("init application logger: %w", err)
	}
	defer logger.Close()

	logger.Debug("application time zone", zap.Any("zone", time.Local))

	logger.Debug("initializing postgres connection pool")
	pool, err := core_pgx_pool.NewConnectionPool(core_pgx_pool.NewConfigMust(), ctx)
	if err != nil {
		return fmt.Errorf("failed to init postgres connection pool: %w", err)
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
		return fmt.Errorf("failed to run migrations: %w", err)
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
		return fmt.Errorf("invalid restaurant integration configuration: %w", err)
	}

	ordersSvc := orders_service.NewOrderService(
		ordersRepo,
		cartRepo,
		menuSvc,
		restaurantsSvc,
		restaurantClient,
	)

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := ordersSvc.RecoverDispatch(ctx, ordersRepo); err != nil {
					logger.Error("order dispatch recovery failed", zap.Error(err))
				}
			}
		}
	}()
	defer func() { cancel(); <-workerDone }()
	restaurantsHandler := restaurants_handler.NewRestaurantHandler(restaurantsSvc)
	menuHandler := menu_handler.NewMenuHandler(menuSvc)
	cartHandler := cart_handler.NewCartHandler(cartSvc)
	ordersHandler := orders_handler.NewOrderHandler(ordersSvc)
	integrationHandler := integrations_handler.NewIntegrationHandler(ordersSvc)

	apiRouter := core_http_server.NewAPIVersionRouter(core_http_server.ApiVersion1)

	ready, err := health.Ready(pool, migrationsDir, ctx)
	if err != nil {
		return fmt.Errorf("initialize readiness: %w", err)
	}

	apiRouter.RegisterRoutes(
		core_http_server.NewRoute(http.MethodGet, "/health", ready),
		core_http_server.NewRoute(http.MethodGet, "/health/live", health.Live),
		core_http_server.NewRoute(http.MethodGet, "/health/ready", ready),
		core_http_server.NewRoute(http.MethodGet, "/restaurants", restaurantsHandler.ListRestaurants),
		core_http_server.NewRoute(http.MethodPost, "/restaurants", restaurantsHandler.CreateRestaurant, auth.Admin()),
		core_http_server.NewRoute(http.MethodGet, "/restaurants/{restaurant_id}", restaurantsHandler.GetRestaurant),
		core_http_server.NewRoute(http.MethodPatch, "/restaurants/{restaurant_id}", restaurantsHandler.UpdateRestaurant, auth.Restaurant(core_http_middleware.RestaurantPath)),
		core_http_server.NewRoute(http.MethodGet, "/restaurants/{restaurant_id}/menu", menuHandler.GetMenu),
		core_http_server.NewRoute(http.MethodPost, "/restaurants/{restaurant_id}/menu/categories", menuHandler.CreateCategory, auth.Restaurant(core_http_middleware.RestaurantPath)),
		core_http_server.NewRoute(http.MethodPost, "/restaurants/{restaurant_id}/menu/items", menuHandler.CreateItem, auth.Restaurant(core_http_middleware.RestaurantPath)),
		core_http_server.NewRoute(http.MethodGet, "/restaurants/{restaurant_id}/orders", ordersHandler.ListRestaurantOrders, auth.Restaurant(core_http_middleware.RestaurantPath)),
		core_http_server.NewRoute(http.MethodGet, "/menu-items/{menu_item_id}", menuHandler.GetMenuItem),
		core_http_server.NewRoute(http.MethodPatch, "/menu-items/{menu_item_id}", menuHandler.UpdateItem, auth.Restaurant(func(r *http.Request) (uuid.UUID, error) {
			id, err := uuid.Parse(r.PathValue("menu_item_id"))
			if err != nil {
				return uuid.Nil, coreerrors.ErrInvalidArgument
			}
			item, err := menuSvc.GetItemByID(r.Context(), id)
			if err != nil {
				return uuid.Nil, err
			}
			return item.RestaurantID, nil
		})),
		core_http_server.NewRoute(http.MethodDelete, "/menu-items/{menu_item_id}", menuHandler.DeleteItem, auth.Restaurant(func(r *http.Request) (uuid.UUID, error) {
			id, err := uuid.Parse(r.PathValue("menu_item_id"))
			if err != nil {
				return uuid.Nil, coreerrors.ErrInvalidArgument
			}
			item, err := menuSvc.GetItemByID(r.Context(), id)
			if err != nil {
				return uuid.Nil, err
			}
			return item.RestaurantID, nil
		})),
		core_http_server.NewRoute(http.MethodGet, "/cart", cartHandler.GetCart,
			auth.User()),
		core_http_server.NewRoute(http.MethodPost, "/cart/items", cartHandler.AddItem,
			auth.User()),
		core_http_server.NewRoute(http.MethodPatch, "/cart/items/{cart_item_id}", cartHandler.UpdateItem,
			auth.User()),
		core_http_server.NewRoute(http.MethodDelete, "/cart/items/{cart_item_id}", cartHandler.RemoveItem,
			auth.User()),
		core_http_server.NewRoute(http.MethodDelete, "/cart", cartHandler.ClearCart,
			auth.User()),
		core_http_server.NewRoute(http.MethodPost, "/orders", ordersHandler.CreateOrder,
			auth.User(),
			core_http_middleware.RequireHeader("Idempotency-Key")),
		core_http_server.NewRoute(http.MethodGet, "/orders", ordersHandler.GetOrders,
			auth.User()),
		core_http_server.NewRoute(http.MethodGet, "/orders/{order_id}", ordersHandler.GetOrder,
			auth.User()),
		core_http_server.NewRoute(http.MethodGet, "/orders/{order_id}/status", ordersHandler.GetOrderStatus,
			auth.User()),
		core_http_server.NewRoute(http.MethodGet, "/orders/{order_id}/history", ordersHandler.GetOrderHistory,
			auth.User()),
		core_http_server.NewRoute(http.MethodPost, "/orders/{order_id}/cancel", ordersHandler.CancelOrder,
			auth.User()),
		core_http_server.NewRoute(http.MethodPost, "/orders/{order_id}/retry", ordersHandler.RetryOrder,
			auth.User()),
		core_http_server.NewRoute(http.MethodPost, "/integrations/restaurants/{restaurant_id}/orders/status", integrationHandler.RestaurantCallback, callbackKeys.Middleware),
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
		return fmt.Errorf("HTTP server: %w", err)
	}

	logger.Debug("shutdown complete")
	return nil
}

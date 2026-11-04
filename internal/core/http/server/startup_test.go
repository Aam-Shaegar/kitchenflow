package core_http_server

import (
	"context"
	logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"go.uber.org/zap"
	"net"
	"testing"
	"time"
)

func TestOccupiedPortIsReturnedAsError(t *testing.T) {
	occupied, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server := NewHTTPServer(Config{Addr: occupied.Addr().String(), ShutdownTimeout: time.Second}, &logger.Logger{Logger: zap.NewNop()})
	if err := server.Run(context.Background()); err == nil {
		t.Fatal("bind failure returned success")
	}
}

package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartupExitCode(t *testing.T) {
	if os.Getenv("KF_TEST_STARTUP_CHILD") == "1" {
		main()
		os.Exit(0)
	}
	database := os.Getenv("KITCHENFLOW_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("set KITCHENFLOW_TEST_DATABASE_URL to disposable PostgreSQL")
	}
	dir := t.TempDir()
	expires := time.Now().Add(time.Hour)
	hash := sha256.Sum256([]byte("temporary-test-credential"))
	write := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	auth := write("auth.json", []map[string]any{{"subject": "test", "role": "user", "token_sha256": hex.EncodeToString(hash[:]), "expires_at": expires}})
	keys := write("keys.json", []map[string]any{{"key_id": "test", "restaurant_id": "550e8400-e29b-41d4-a716-446655440000", "secret_base64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "expires_at": expires}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.Addr().String()
	migrations, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartupExitCode$")
	child.Env = append(os.Environ(), "KF_TEST_STARTUP_CHILD=1", "AUTH_CREDENTIALS_FILE="+auth, "CALLBACK_KEYS_FILE="+keys, "DATABASE_URL="+database, "DB_TIMEOUT=10s", "SERVER_ADDR="+addr, "HTTP_SERVER_ADDR="+addr, "LOGGER_FOLDER="+filepath.Join(dir, "logs"), "MIGRATIONS_DIR="+migrations)
	output, err := child.CombinedOutput()
	if err == nil {
		t.Fatal("startup failure returned successful exit")
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !strings.Contains(string(output), "HTTP server: listen HTTP:") {
		t.Fatalf("unexpected exit: %v output %s", err, output)
	}
}

package core_http_server

import "time"

// Config is supplied by the application configuration at startup.
type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
}

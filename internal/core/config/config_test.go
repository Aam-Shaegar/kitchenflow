package core_config

import "testing"

func TestInvalidRuntimeConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	for _, tc := range []struct{ key, value string }{{"SERVER_ADDR", "bad"}, {"SERVER_ADDR", ":-1"}, {"SERVER_ADDR", ":70000"}, {"SHUTDOWN_TIMEOUT", "0s"}, {"SHUTDOWN_TIMEOUT", "-1s"}, {"TIME_ZONE", "not-a-zone"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := NewConfig(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

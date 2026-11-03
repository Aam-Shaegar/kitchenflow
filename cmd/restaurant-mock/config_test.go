package main

import "testing"

func TestMockConfigRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"MOCK_MODE", "unknown"}, {"MOCK_DELAYS", "garbage"}, {"MOCK_DELAYS", "-1s"}, {"MOCK_DELAYS", "2s,bad"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := configFromEnv(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

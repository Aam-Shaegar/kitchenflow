package core_http_request

import (
	"errors"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONBoundaries(t *testing.T) {
	for _, tc := range []struct {
		body string
		want error
	}{
		{`{"name":"ok"} `, nil}, {`{"name":"ok"} {}`, coreerrors.ErrInvalidArgument}, {`{"name":"ok"} junk`, coreerrors.ErrInvalidArgument}, {`{"name":"ok"} null`, coreerrors.ErrInvalidArgument}, {`null`, coreerrors.ErrInvalidArgument}, {`[]`, coreerrors.ErrInvalidArgument}, {`{"name":null}`, coreerrors.ErrInvalidArgument}, {`{"unknown":1}`, coreerrors.ErrInvalidArgument}, {`{"name":"` + strings.Repeat("x", int(MaxBodyBytes)) + `"}`, coreerrors.ErrRequestTooLarge},
	} {
		for _, validated := range []bool{false, true} {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			var value struct {
				Name string `json:"name"`
			}
			var err error
			if validated {
				err = DecodeAndValidateRequest(req, &value)
			} else {
				err = DecodeObject(req, &value)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("validated=%t error %v want %v", validated, err, tc.want)
			}
		}
	}
}

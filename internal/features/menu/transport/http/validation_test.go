package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	logger "github.com/Aam-Shaegar/kitchenflow/internal/core/logger"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/repository"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/service"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type validationRepo struct {
	repository.MenuRepository
	rid, cat uuid.UUID
	saved    *domain.MenuItem
}

func (r *validationRepo) GetCategoryByID(context.Context, uuid.UUID) (*domain.MenuCategory, error) {
	return &domain.MenuCategory{ID: r.cat, RestaurantID: r.rid}, nil
}
func (r *validationRepo) GetItemByID(context.Context, uuid.UUID) (*domain.MenuItem, error) {
	return &domain.MenuItem{ID: uuid.New(), RestaurantID: r.rid, CategoryID: r.cat, Name: "Item", Price: 100}, nil
}
func (r *validationRepo) CreateItem(_ context.Context, item *domain.MenuItem) error {
	r.saved = item
	return nil
}
func (r *validationRepo) UpdateItem(_ context.Context, item *domain.MenuItem) error {
	r.saved = item
	return nil
}
func TestMenuRequestValidation(t *testing.T) {
	for _, patch := range []bool{false, true} {
		cases := []struct {
			body string
			code int
		}{
			{`{"price":0}`, 200}, {`{"price":-1}`, 400}, {`{"price":1000000001}`, 400},
			{`{"price":null}`, 400}, {`{"price":1.5}`, 400}, {`{"unknown":1}`, 400}, {`null`, 400},
			{`{"name":"   "}`, 400}, {`{"description":"` + strings.Repeat("a", 2001) + `"}`, 400},
			{`{"name":"` + strings.Repeat("я", 200) + `","price":0}`, 200},
		}
		if !patch {
			cases = append(cases, struct {
				body string
				code int
			}{`{}`, 400}, struct {
				body string
				code int
			}{`{"external_id":"` + strings.Repeat("x", 129) + `","price":1}`, 400})
		}
		for _, tc := range cases {
			t.Run(fmt.Sprintf("patch=%t/%s", patch, tc.body[:min(len(tc.body), 30)]), func(t *testing.T) {
				repo := &validationRepo{rid: uuid.New(), cat: uuid.New()}
				handler := NewMenuHandler(service.NewMenuService(repo))
				body := tc.body

				if !patch {
					var fields map[string]json.RawMessage
					if json.Unmarshal([]byte(body), &fields) == nil && fields != nil {
						fields["category_id"] = json.RawMessage(`"` + repo.cat.String() + `"`)
						if _, ok := fields["name"]; !ok {
							fields["name"] = json.RawMessage(`"Item"`)
						}
						if _, ok := fields["price"]; !ok && tc.body != "{}" {
							fields["price"] = json.RawMessage(`1`)
						}
						raw, err := json.Marshal(fields)
						if err != nil {
							t.Fatal(err)
						}
						body = string(raw)
					}
				}

				req := httptest.NewRequest("POST", "/", strings.NewReader(body))
				req.SetPathValue("restaurant_id", repo.rid.String())
				req.SetPathValue("menu_item_id", uuid.NewString())
				req = req.WithContext(logger.ToContext(req.Context(), &logger.Logger{Logger: zap.NewNop()}))
				rw := httptest.NewRecorder()
				if patch {
					handler.UpdateItem(rw, req)
				} else {
					handler.CreateItem(rw, req)
				}
				want := tc.code
				if !patch && want == 200 {
					want = 201
				}
				if rw.Code != want {
					t.Fatalf("HTTP %d want %d body %s response %s", rw.Code, want, body, rw.Body.String())
				}
				if want < 300 && strings.Contains(tc.body, `"price":0`) && (repo.saved == nil || repo.saved.Price != 0) {
					t.Fatal("zero price not saved")
				}
			})
		}
	}
}

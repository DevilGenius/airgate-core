package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
)

type paginationRouteRepository struct {
	usageRouteRepoStub
	filters chan appusage.ListFilter
}

func (r *paginationRouteRepository) BuildPageIndex(_ context.Context, filter appusage.ListFilter) (*appusage.PageIndex, error) {
	r.filters <- filter
	index := &appusage.PageIndex{}
	return index, index.AddID(50)
}

func TestUserPaginationForcesAuthenticatedOwnerAndAPIKey(t *testing.T) {
	repo := &paginationRouteRepository{filters: make(chan appusage.ListFilter, 1)}
	handler := NewUsageHandler(appusage.NewService(repo))
	w := invokeHandlerForValidation(http.MethodGet, "/usage/pagination?page=1&page_size=20&user_id=999&api_key_id=888&account=ignored", "", nil, func(c *gin.Context) { c.Set("user_id", 7); c.Set(middleware.CtxKeyAPIKeyID, 42) }, handler.UserUsagePagination)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	select {
	case filter := <-repo.filters:
		if filter.UserID == nil || *filter.UserID != 7 || filter.APIKeyID == nil || *filter.APIKeyID != 42 || !filter.ScopedToKey || filter.AccountSearch != "" {
			t.Fatalf("untrusted scope=%+v", filter)
		}
	case <-time.After(time.Second):
		t.Fatal("pagination scope not passed to repository")
	}
	w = invokeHandlerForValidation(http.MethodGet, "/usage/pagination?page=1&page_size=20", "", nil, nil, handler.UserUsagePagination)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", w.Code)
	}
}

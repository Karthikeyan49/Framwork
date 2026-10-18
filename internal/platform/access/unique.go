package access

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

var uniquePOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(versions/[0-9]+|datasets/[0-9a-f-]{36}/bindings/[0-9]+)/entities/entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/unique-constraints/field\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(register|build|abort)$`)

func uniqueRequest(ctx context.Context, q postgres.Executor, r *http.Request, s data.Scope, a tenant.Access, engine policy.Engine, g data.Guard, resolve data.ScopeResolver) (reply, error) {
	e, id := r.PathValue("entity"), r.PathValue("index")
	if err := data.UniqueAuthority(ctx, engine, e, id, g, resolve); err != nil {
		return indexReply(data.UniqueConstraint{}, err)
	}
	st := data.Store{}
	if r.Method == http.MethodGet {
		x, err := st.UniqueConstraint(ctx, q, s, a, e, id)
		return indexReply(x, err)
	}
	if strings.HasSuffix(r.URL.Path, "/register") {
		var body struct {
			ExpectedGeneration *int64 `json:"expected_generation"`
		}
		if readDataCommand(r, &body) != nil || body.ExpectedGeneration == nil {
			return indexReply(data.UniqueConstraint{}, data.ErrInvalid)
		}
		x, err := st.RegisterUniqueConstraint(ctx, q, s, a, e, id, *body.ExpectedGeneration)
		return indexReply(x, err)
	}
	var body struct {
		ExpectedGeneration int64 `json:"expected_generation"`
		ExpectedCheckpoint int64 `json:"expected_checkpoint"`
	}
	if readDataCommand(r, &body) != nil {
		return indexReply(data.UniqueConstraint{}, data.ErrInvalid)
	}
	if strings.HasSuffix(r.URL.Path, "/abort") {
		x, err := st.AbortUniqueConstraint(ctx, q, s, a, e, id, body.ExpectedGeneration, body.ExpectedCheckpoint)
		return indexReply(x, err)
	}
	x, err := st.BuildUniqueConstraint(ctx, q, s, a, e, id, body.ExpectedGeneration, body.ExpectedCheckpoint)
	return indexReply(x, err)
}

package access

import (
	"context"
	"net/http"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// The three human decisions on a stored proposal (ADR040 A9). Each is one ordinary
// transaction with the audit row of the standard envelope; each additionally
// requires the capability of the underlying write, so ai.apply alone never bypasses
// metadata.write or requirements.decide.

// aiApplyView is the answer to a successful apply.
type aiApplyView struct {
	Proposal  ai.Proposal      `json:"proposal"`
	Revision  metadata.Version `json:"revision"`
	Sensitive []string         `json:"sensitive"`
}

func (a API) aiApply(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, app, id string, evidence *audit.Entry) (reply, error) {
	var body struct {
		ExpectedRevision int64    `json:"expected_revision"`
		Acknowledge      []string `json:"acknowledge"`
	}
	if rep, ok := aiBody(r, aiApplyBody, &body, []string{"expected_revision", "acknowledge"}); !ok {
		return rep, nil
	}
	if body.ExpectedRevision < 1 || body.ExpectedRevision > 1_000_000_000 || body.Acknowledge == nil || len(body.Acknowledge) > 512 {
		return aiDeny(400, "invalid_ai_request", "Request body is invalid"), nil
	}
	if err := a.aiCompose(ctx, u, access, "metadata.write", "metadata", app, evidence); err != nil {
		return reply{}, err
	}
	_, _, base, err := a.activeBase(ctx, q, access, app)
	if err != nil {
		return reply{}, err
	}
	in := ai.ApplyInput{App: app, ProposalID: id, ExpectedRevision: body.ExpectedRevision, Acknowledge: body.Acknowledge, Compile: a.aiCompile()}
	if base.Valid() {
		in.Base = &base
	}
	res, err := a.AI.Service.Apply(ctx, q, access, in)
	if err != nil {
		return aiReply(r, err)
	}
	aiEvidence(evidence, res.Proposal)
	evidence.Action = "ai.proposal.applied"
	evidence.State.AIAckHash = res.AckHash
	metadataEvidence(ctx, evidence, res.Revision)
	return reply{status: 200, body: aiApplyView{Proposal: res.Proposal, Revision: res.Revision, Sensitive: res.Sensitive}}, nil
}

// aiConfirmView is the answer to a successful confirm.
type aiConfirmView struct {
	Proposal     ai.Proposal                  `json:"proposal"`
	Requirements promotion.StoredRequirements `json:"requirements"`
	Recorded     []ai.RecordedRequirement     `json:"recorded"`
}

func (a API) aiConfirm(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, app, id string, evidence *audit.Entry) (reply, error) {
	var body struct {
		ExpectedVersion int64                `json:"expected_requirements_version"`
		Decisions       []ai.ConfirmDecision `json:"decisions"`
	}
	if rep, ok := aiBody(r, aiConfirmBody, &body, []string{"expected_requirements_version", "decisions"}); !ok {
		return rep, nil
	}
	if body.ExpectedVersion < 0 || body.ExpectedVersion >= 1_000_000 || body.Decisions == nil || len(body.Decisions) > ai.MaxListItems {
		return aiDeny(400, "invalid_ai_request", "Request body is invalid"), nil
	}
	if err := a.aiCompose(ctx, u, access, "requirements.decide", "preview", app, evidence); err != nil {
		return reply{}, err
	}
	res, err := a.AI.Service.Confirm(ctx, q, access, ai.ConfirmInput{App: app, ProposalID: id, ExpectedVersion: body.ExpectedVersion, Decisions: body.Decisions})
	if err != nil {
		return aiReply(r, err)
	}
	aiEvidence(evidence, res.Proposal)
	evidence.Action = "ai.proposal.confirmed"
	evidence.State.RequirementsVersion, evidence.State.ManifestHash = res.Requirements.Version, res.Requirements.Hash
	return reply{status: 200, body: aiConfirmView{Proposal: res.Proposal, Requirements: res.Requirements, Recorded: res.Recorded}}, nil
}

func (a API) aiReject(ctx context.Context, q postgres.Executor, r *http.Request, access tenant.Access, app, id string, evidence *audit.Entry) (reply, error) {
	var body struct {
		Reason string `json:"reason"`
	}
	if rep, ok := aiBody(r, aiRejectBody, &body, []string{"reason"}); !ok {
		return rep, nil
	}
	p, err := a.AI.Service.Reject(ctx, q, access, ai.RejectInput{App: app, ProposalID: id, Reason: body.Reason})
	if err != nil {
		return aiReply(r, err)
	}
	aiEvidence(evidence, p)
	evidence.Action = "ai.proposal.discarded"
	return reply{status: 200, body: struct {
		Proposal ai.Proposal `json:"proposal"`
	}{p}}, nil
}

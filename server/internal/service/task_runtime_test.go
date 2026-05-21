package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestResolveIssueRuntimeID covers the per-issue runtime override decision:
// when issue.runtime_id is set, the resolver returns it instead of the
// agent's default; when issue.runtime_id is NULL, it falls back to the
// agent's default. The resolver itself doesn't validate; it trusts the
// upstream write-path (canUseRuntimeForAgent) and the FK ON DELETE SET NULL
// to keep dangling references out.
func TestResolveIssueRuntimeID(t *testing.T) {
	mkUUID := func(b byte) pgtype.UUID {
		var u pgtype.UUID
		for i := range u.Bytes {
			u.Bytes[i] = b
		}
		u.Valid = true
		return u
	}
	agentRT := mkUUID(0xA)
	issueRT := mkUUID(0xB)

	cases := []struct {
		desc       string
		issueRT    pgtype.UUID
		agentRT    pgtype.UUID
		wantBytes  byte
		wantValid  bool
	}{
		{
			desc:      "no_pin_uses_agent_default",
			issueRT:   pgtype.UUID{},      // Valid: false
			agentRT:   agentRT,
			wantBytes: 0xA,
			wantValid: true,
		},
		{
			desc:      "pin_overrides_agent",
			issueRT:   issueRT,
			agentRT:   agentRT,
			wantBytes: 0xB,
			wantValid: true,
		},
		{
			desc:      "pin_set_but_agent_unset_still_uses_pin",
			issueRT:   issueRT,
			agentRT:   pgtype.UUID{},      // shouldn't happen in practice; guard regression
			wantBytes: 0xB,
			wantValid: true,
		},
		{
			desc:      "both_unset_returns_invalid",
			issueRT:   pgtype.UUID{},
			agentRT:   pgtype.UUID{},
			wantBytes: 0,
			wantValid: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := resolveIssueRuntimeID(
				db.Issue{RuntimeID: tc.issueRT},
				db.Agent{RuntimeID: tc.agentRT},
			)
			if got.Valid != tc.wantValid {
				t.Fatalf("Valid: got %v, want %v", got.Valid, tc.wantValid)
			}
			if tc.wantValid && got.Bytes[0] != tc.wantBytes {
				t.Fatalf("Bytes[0]: got %x, want %x", got.Bytes[0], tc.wantBytes)
			}
		})
	}
}

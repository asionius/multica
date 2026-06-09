package daemon

import (
	"strings"
	"testing"
)

// TestBuildQuickCreatePromptRules locks in the rules that govern how the
// quick-create agent is allowed to translate raw user input into the issue
// description body. Each substring corresponds to a concrete failure mode
// observed in production output:
//   - meta-instructions ("create an issue", "cc @X") leaking into the body
//   - the Context section being misused as an apology log when no external
//     references were actually fetched
//   - hard-line rules being silently dropped on prompt rewrites
func TestBuildQuickCreatePromptRules(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})

	mustContain := []string{
		// high-fidelity invariant
		"Faithfully restate what the user wants",
		"Preserve specific names, identifiers, file paths",
		// strip non-spec material: verbal routing wrappers + conversational fillers
		"verbal routing wrappers about creating the issue",
		"pure conversational fillers",
		// cc routing must survive: mention link stays in description so the
		// auto-subscribe path fires (multica issue create has no --subscriber flag)
		"CC exception",
		"auto-subscribes members",
		// context section is conditional and must not be an apology log
		"include ONLY when the input cited external resources",
		"never use it as an apology log",
		// output/reporting must be workspace-prefix agnostic. Workspaces can
		// use custom issue prefixes, so a successful issue creation should
		// not look failed merely because the identifier does not match one
		// fixed prefix.
		"multica issue create --output json",
		"JSON response",
		"identifier",
		"Do not scrape human output",
		"do not assume any workspace issue prefix",
		"Created <identifier-or-id>: <title>",
		// hard rules
		"never invent requirements",
		"never reduce multi-sentence input",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt output missing required rule: %q", s)
		}
	}
}

// TestBuildQuickCreatePromptAssigneeIncludesSquads locks in the MUL-2165
// fix: the assignee-resolution rules must tell the agent to consult the
// squad list alongside members and agents. Before this, a quick-create
// input like "assign to <SquadName>" silently fell through to
// "Unrecognized assignee" because squads were never queried.
func TestBuildQuickCreatePromptAssigneeIncludesSquads(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	mustContain := []string{
		"multica squad list",
		"Squads are first-class assignees",
		"Treat bare @-routing as an assignee directive",
		"让 @独立团 review 这个 PR",
		"pass the squad's `id` as `--assignee-id`",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt assignee block missing %q\n--- output ---\n%s", s, out)
		}
	}
}

// TestBuildQuickCreatePromptSquadDefaultsToSquad locks in the MUL-2203
// fix: when the picker was a squad, the task runs on the squad's leader
// agent, but the default assignee for issues created by this run must
// point at the SQUAD's UUID — not the leader agent's UUID. The previous
// "default to YOURSELF" instruction made squad-created issues land under
// the leader, hiding them from the squad's delegation flow.
func TestBuildQuickCreatePromptSquadDefaultsToSquad(t *testing.T) {
	const (
		squadID   = "aaaa1111-2222-3333-4444-555555555555"
		squadName = "独立团"
		leaderID  = "bbbb1111-2222-3333-4444-666666666666"
	)
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		Agent:             &AgentData{ID: leaderID, Name: "leader-agent"},
		SquadID:           squadID,
		SquadName:         squadName,
	})

	// The default-assignee instruction must point at the squad UUID.
	if !strings.Contains(out, "--assignee-id \""+squadID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must default to the squad's UUID, got:\n%s", out)
	}
	// And it must NOT tell the agent to default to itself (the leader).
	if strings.Contains(out, "--assignee-id \""+leaderID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must NOT default to the leader agent's UUID, got:\n%s", out)
	}
	// The squad name should appear in the instruction so the agent has
	// human-readable context for the routing decision.
	if !strings.Contains(out, squadName) {
		t.Errorf("buildQuickCreatePrompt with SquadID should mention the squad name %q, got:\n%s", squadName, out)
	}
	// And the prompt must explicitly call out the squad-vs-leader rule
	// so the agent does not silently regress to "default to YOURSELF".
	mustContain := []string{
		"picker SQUAD",
		"running on the squad's behalf",
		"do not assign it to your own agent UUID",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with SquadID missing %q\n--- output ---\n%s", s, out)
		}
	}
}

// TestBuildQuickCreatePromptProjectPinning verifies that when the user
// pins a project in the quick-create modal, the prompt instructs the agent
// to pass `--project <uuid>` exactly. Without this, the agent would re-read
// the workspace default and silently drop the user's selection — the same
// "I have to retype 'in project X' every time" failure mode the modal
// addition was meant to fix.
func TestBuildQuickCreatePromptProjectPinning(t *testing.T) {
	const projectID = "11111111-2222-3333-4444-555555555555"
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ProjectID:         projectID,
		ProjectTitle:      "Web App",
	})
	mustContain := []string{
		"--project \"" + projectID + "\"",
		"Web App",
		"modal selection is authoritative",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with project missing %q\n--- output ---\n%s", s, out)
		}
	}

	// Without a project, the prompt must keep the legacy "omit" instruction
	// so the agent doesn't accidentally start passing --project on plain
	// quick-create runs.
	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if !strings.Contains(plain, "**project**: omit") {
		t.Errorf("buildQuickCreatePrompt without project must keep the omit instruction, got:\n%s", plain)
	}
	if strings.Contains(plain, "--project") {
		t.Errorf("buildQuickCreatePrompt without project must NOT mention --project, got:\n%s", plain)
	}
}

// TestBuildPromptSquadLeaderNoActionForMemberTrigger verifies that the
// squad leader no_action prohibition is injected in the per-turn prompt
// regardless of whether the triggering comment was posted by an agent or
// a member. This was the root cause of the "LGTM is a pure acknowledgment
// — no reply needed. Exiting silently." noise comment: the prohibition
// only fired for agent-triggered comments, so member-triggered ones
// (like "LGTM") bypassed it.
func TestBuildPromptSquadLeaderNoActionForMemberTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for member-triggered comments, got:\n%s", out)
	}
	if !strings.Contains(out, "DO NOT post any comment") {
		t.Errorf("buildCommentPrompt must contain DO NOT post prohibition for member-triggered squad leader, got:\n%s", out)
	}
}

// TestBuildPromptSquadLeaderNoActionForAgentTrigger verifies the rule also
// fires for agent-triggered comments (the original path that already worked).
func TestBuildPromptSquadLeaderNoActionForAgentTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "Deploy complete.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "deploy-boy",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for agent-triggered comments, got:\n%s", out)
	}
}

// TestBuildPromptNonSquadLeaderNoRule verifies that non-squad-leader agents
// do NOT get the squad leader no_action rule injected.
func TestBuildPromptNonSquadLeaderNoRule(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions without the squad marker",
		},
	}
	out := BuildPrompt(task, "claude")
	if strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must NOT inject squad leader no_action rule for non-squad-leader agents, got:\n%s", out)
	}
}

// TestRenderPipelineStateLegacy locks in that legacy issues (kind = "")
// get an empty render. The agent's description-parse fallback handles
// those — injecting an empty header would just confuse the prompt.
func TestRenderPipelineStateLegacy(t *testing.T) {
	out := renderPipelinePrefix(Task{IssueID: "issue-123"})
	if out != "" {
		t.Errorf("legacy issues (no pipeline_kind) must render empty, got:\n%s", out)
	}
}

// TestRenderPipelineStateMiddleStage exercises the most common path:
// a designer (middle of the 5-stage pipeline) with active counters.
// The prefix must surface role/prev/next/parent/version/branch/cap,
// and the dynamic suffix must carry the counter values.
func TestRenderPipelineStateMiddleStage(t *testing.T) {
	base := `{
        "siblings": [
            {"role":"requirement","id":"id-req"},
            {"role":"designer","id":"id-dsn"},
            {"role":"coder","id":"id-cdr"},
            {"role":"tester","id":"id-tst"},
            {"role":"integrator","id":"id-int"}
        ],
        "top_issue_id": "id-top",
        "version": "v1.2.3",
        "branch": "v1.2.3/abc",
        "bounce_cap": 5
    }`
	dyn := `{"bounce_with_prev":1,"bounce_with_next":0,"escalated":false}`
	task := Task{
		IssueID:         "id-dsn",
		PipelineKind:    "ccproc-stage",
		PipelineBase:    []byte(base),
		PipelineDynamic: []byte(dyn),
		Agent:           &AgentData{Name: "CC-designer"},
	}

	// Static prefix checks: topology + governance, never changes.
	prefix := renderPipelinePrefix(task)
	mustContain := []string{
		"pipeline_kind: ccproc-stage",
		"role: designer",
		"prev_id: id-req",
		"next_id: id-cdr",
		"parent_id: id-top",
		"version: v1.2.3",
		"branch: v1.2.3/abc",
		"bounce_cap: 5",
		"multica issue verdict",
		"NEVER 调用 `multica issue pipeline get`",
	}
	for _, want := range mustContain {
		if !strings.Contains(prefix, want) {
			t.Errorf("prefix render missing %q, got:\n%s", want, prefix)
		}
	}

	// Dynamic suffix checks: counters must appear (and appear at the end).
	dynOut := renderPipelineDynamic(task)
	dynMust := []string{
		"counter.bounce_with_prev: 1",
		"counter.bounce_with_next: 0",
		"counter.escalated: false",
	}
	for _, want := range dynMust {
		if !strings.Contains(dynOut, want) {
			t.Errorf("dynamic render missing %q, got:\n%s", want, dynOut)
		}
	}
}

// TestRenderPipelineStateBoundaries verifies the prev/next pointers go
// empty at the start (requirement) and end (integrator) of the pipeline
// instead of wrapping or pointing at a sibling that doesn't exist.
func TestRenderPipelineStateBoundaries(t *testing.T) {
	base := `{
        "siblings": [
            {"role":"requirement","id":"id-req"},
            {"role":"designer","id":"id-dsn"},
            {"role":"integrator","id":"id-int"}
        ],
        "top_issue_id": "id-top",
        "version": "v1.0.0",
        "branch": "v1.0.0/x",
        "bounce_cap": 3
    }`
	// First stage: prev should be empty.
	first := renderPipelinePrefix(Task{
		IssueID:      "id-req",
		PipelineKind: "ccproc-stage",
		PipelineBase: []byte(base),
		Agent:        &AgentData{Name: "CC-requirement"},
	})
	if !strings.Contains(first, "prev_id: \n") {
		t.Errorf("first stage must render empty prev_id, got:\n%s", first)
	}
	if !strings.Contains(first, "next_id: id-dsn") {
		t.Errorf("first stage next_id wrong, got:\n%s", first)
	}
	// Last stage: next should be empty.
	last := renderPipelinePrefix(Task{
		IssueID:      "id-int",
		PipelineKind: "ccproc-stage",
		PipelineBase: []byte(base),
		Agent:        &AgentData{Name: "CC-integrator"},
	})
	if !strings.Contains(last, "next_id: \n") {
		t.Errorf("last stage must render empty next_id, got:\n%s", last)
	}
	if !strings.Contains(last, "prev_id: id-dsn") {
		t.Errorf("last stage prev_id wrong, got:\n%s", last)
	}
}

// TestRenderPipelineStateTopAndPackager covers the two non-stage kinds
// (top and packager): they don't have a siblings array, so the
// role/prev/next lines should be omitted, but version/branch/bounce_cap
// still surface from base. The dynamic block is empty by design — top
// and packager don't ping-pong — so no dynamic lines appear.
func TestRenderPipelineStateTopAndPackager(t *testing.T) {
	for _, kind := range []string{"ccproc-top", "ccproc-packager"} {
		base := `{"version":"v9.9.9","branch":"v9.9.9/zzz","bounce_cap":7}`
		out := renderPipelinePrefix(Task{
			IssueID:      "id-anything",
			PipelineKind: kind,
			PipelineBase: []byte(base),
		})
		if !strings.Contains(out, "pipeline_kind: "+kind) {
			t.Errorf("%s render missing pipeline_kind line:\n%s", kind, out)
		}
		if strings.Contains(out, "role:") || strings.Contains(out, "prev_id:") || strings.Contains(out, "next_id:") {
			t.Errorf("%s render must NOT include role/prev/next lines:\n%s", kind, out)
		}
		for _, want := range []string{"version: v9.9.9", "branch: v9.9.9/zzz", "bounce_cap: 7"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s render missing %q:\n%s", kind, want, out)
			}
		}
	}
}

// TestRenderPipelineStateExposesAllSiblings verifies the renderer
// surfaces every sibling by role, not just the immediate prev/next.
// Tester / integrator need this to peek across multiple stages
// (e.g. tester reads requirement+designer+coder outputs) without
// shelling back to the CLI.
func TestRenderPipelineStateExposesAllSiblings(t *testing.T) {
	base := `{
        "siblings": [
            {"role":"requirement","id":"id-req"},
            {"role":"designer","id":"id-dsn"},
            {"role":"coder","id":"id-cdr"},
            {"role":"tester","id":"id-tst"},
            {"role":"integrator","id":"id-int"}
        ],
        "top_issue_id": "id-top",
        "version": "v1.0.0",
        "branch": "v1.0.0/x",
        "bounce_cap": 3
    }`
	out := renderPipelinePrefix(Task{
		IssueID:      "id-tst",
		PipelineKind: "ccproc-stage",
		PipelineBase: []byte(base),
		Agent:        &AgentData{Name: "CC-tester"},
	})
	for _, want := range []string{
		"siblings.requirement: id-req",
		"siblings.designer: id-dsn",
		"siblings.coder: id-cdr",
		"siblings.tester: id-tst",
		"siblings.integrator: id-int",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q (multi-upstream consumers need every sibling), got:\n%s", want, out)
		}
	}
}

// TestBuildPromptInjectsPipelineForCommentTask verifies the comment
// path also gets the pipeline header — comment-triggered turns are the
// dominant CCProc invocation pattern (every ping-pong is a new comment).
func TestBuildPromptInjectsPipelineForCommentTask(t *testing.T) {
	task := Task{
		IssueID:               "id-dsn",
		TriggerCommentID:      "c1",
		TriggerCommentContent: "please review",
		TriggerAuthorType:     "member",
		PipelineKind:          "ccproc-stage",
		PipelineBase:          []byte(`{"siblings":[{"role":"designer","id":"id-dsn"},{"role":"coder","id":"id-cdr"}],"top_issue_id":"id-top","version":"v1","branch":"v1/x","bounce_cap":3}`),
		PipelineDynamic:       []byte(`{"bounce_with_prev":0,"bounce_with_next":0,"escalated":false}`),
		Agent:                 &AgentData{Name: "CC-designer"},
	}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "Pipeline 状态") {
		t.Errorf("comment-triggered prompt must inject pipeline header, got:\n%s", out)
	}
	if !strings.Contains(out, "next_id: id-cdr") {
		t.Errorf("comment-triggered prompt must surface resolved neighbors, got:\n%s", out)
	}
}

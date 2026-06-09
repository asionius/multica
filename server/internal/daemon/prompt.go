package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig. The provider string is used by
// comment-triggered tasks: Codex's per-turn reply template needs the
// platform-aware "stdin or file" variant, every other provider gets a
// lightweight inline template (or Windows file for any provider on
// Windows).
func BuildPrompt(task Task, provider string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task, provider)
	}
	if task.AutopilotRunID != "" {
		return buildAutopilotPrompt(task)
	}
	if task.QuickCreatePrompt != "" {
		return buildQuickCreatePrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if prefix := renderPipelinePrefix(task); prefix != "" {
		b.WriteString(prefix)
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	fmt.Fprintf(&b, "If you need comment history, `multica issue comment list %s --output json` returns all comments for the issue (server caps at 2000). Pass `--since <RFC3339>` to fetch only comments newer than a known cursor.\n", task.IssueID)
	// Dynamic counters go last so they don't shift the cache boundary.
	if dyn := renderPipelineDynamic(task); dyn != "" {
		b.WriteString(dyn)
	}
	return b.String()
}

// renderPipelinePrefix materializes the STATIC half of the pipeline
// state (migration 097) into a prompt block — topology, role assignment,
// and governance parameters that never change once the pipeline is
// created. Keeping this stable is critical for prompt prefix cache
// hits across --resume turns.
//
// Dynamic counters (bounce_with_prev / bounce_with_next / escalated)
// are handled separately by renderPipelineDynamic and must appear at
// the END of the prompt so they don't shift the cache boundary.
//
// Returns empty string for legacy issues (kind = "").
func renderPipelinePrefix(task Task) string {
	if task.PipelineKind == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Pipeline 状态(daemon 已为你解析,直接使用以下变量)\n\n")
	fmt.Fprintf(&b, "- pipeline_kind: %s\n", task.PipelineKind)

	var base map[string]any
	if len(task.PipelineBase) > 0 {
		if err := json.Unmarshal(task.PipelineBase, &base); err != nil {
			base = nil
		}
	}

	// ccproc-stage: resolve role/prev/next from siblings ordered array.
	if task.PipelineKind == "ccproc-stage" && base != nil {
		role := agentRole(task)
		if role != "" {
			fmt.Fprintf(&b, "- role: %s\n", role)
		}
		prev, next := resolveNeighbors(base, role)
		fmt.Fprintf(&b, "- prev_id: %s\n", prev)
		fmt.Fprintf(&b, "- next_id: %s\n", next)
		if v, ok := base["top_issue_id"].(string); ok {
			fmt.Fprintf(&b, "- parent_id: %s\n", v)
		}
		for _, kv := range siblingPairs(base) {
			fmt.Fprintf(&b, "- siblings.%s: %s\n", kv.role, kv.id)
		}
	}

	if base != nil {
		if v, ok := base["version"].(string); ok {
			fmt.Fprintf(&b, "- version: %s\n", v)
		}
		if v, ok := base["branch"].(string); ok {
			fmt.Fprintf(&b, "- branch: %s\n", v)
		}
		if v, ok := base["bounce_cap"]; ok {
			fmt.Fprintf(&b, "- bounce_cap: %v\n", numericOrAny(v))
		}
	}

	b.WriteString("\n需要写回动态计数器时调用 verdict API:\n")
	fmt.Fprintf(&b, "  multica issue verdict %s --action <approve|deny|escalate> [--feedback \"...\"]\n", task.IssueID)
	b.WriteString("(读侧 NEVER 调用 `multica issue pipeline get` — 上面的字段就是权威源。)\n\n")

	b.WriteString("需要完整 base JSON 时可读(通常不需要):\n")
	b.WriteString("  multica issue pipeline get <issue-id> --field base\n\n")
	return b.String()
}

// renderPipelineDynamic materializes the VARIABLE half of the pipeline
// state — counters that change on every approval/denial cycle. This
// block MUST appear at the END of the daemon prompt so that the static
// prefix (renderPipelinePrefix + instructions) stays identical across
// --resume turns and keeps the prompt cache warm.
//
// Returns empty string when there is no pipeline or no dynamic data
// (optional counters like bounce_with_prev may be absent).
func renderPipelineDynamic(task Task) string {
	if task.PipelineKind == "" {
		return ""
	}
	if len(task.PipelineDynamic) == 0 {
		return ""
	}
	var dyn map[string]any
	if err := json.Unmarshal(task.PipelineDynamic, &dyn); err != nil {
		return ""
	}
	var b strings.Builder
	hasAny := false
	if v, ok := dyn["bounce_with_prev"]; ok {
		fmt.Fprintf(&b, "- counter.bounce_with_prev: %v\n", numericOrAny(v))
		hasAny = true
	}
	if v, ok := dyn["bounce_with_next"]; ok {
		fmt.Fprintf(&b, "- counter.bounce_with_next: %v\n", numericOrAny(v))
		hasAny = true
	}
	if v, ok := dyn["escalated"]; ok {
		fmt.Fprintf(&b, "- counter.escalated: %v\n", v)
		hasAny = true
	}
	if !hasAny {
		return ""
	}
	return "## Pipeline 计数器(当前值)\n" + b.String() + "\n"
}

// agentRole strips the leading "CC-" prefix from the assigned agent's
// name to produce a short stage role (e.g. "CC-designer" → "designer").
// The CCProc convention uses these short forms as keys into the
// pipeline_base.siblings ordered array. Returns "" when the task has
// no agent or the name doesn't match the CC- pattern — the caller then
// falls back to omitting the role/prev/next lines.
func agentRole(task Task) string {
	if task.Agent == nil {
		return ""
	}
	name := strings.TrimSpace(task.Agent.Name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "CC-") {
		return strings.TrimPrefix(name, "CC-")
	}
	if strings.HasPrefix(name, "cc-") {
		return strings.TrimPrefix(name, "cc-")
	}
	return ""
}

// resolveNeighbors walks pipeline_base.siblings (an ordered JSON array
// of {role, id} objects) to find the entries immediately before and
// after the given role. Returns ("", "") when role is empty, siblings
// is missing, or the role doesn't appear in the array. Either neighbor
// may be empty when the agent is at a stage boundary (start/end).
func resolveNeighbors(base map[string]any, role string) (string, string) {
	if role == "" {
		return "", ""
	}
	pairs := siblingPairs(base)
	if len(pairs) == 0 {
		return "", ""
	}
	idx := -1
	for i, kv := range pairs {
		if kv.role == role {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", ""
	}
	prev := ""
	next := ""
	if idx > 0 {
		prev = pairs[idx-1].id
	}
	if idx < len(pairs)-1 {
		next = pairs[idx+1].id
	}
	return prev, next
}

// siblingPair is one entry of pipeline_base.siblings — a stage role and
// the issue id of the corresponding sub-issue. Pulled out as a typed
// helper so resolveNeighbors and the prompt renderer share one parsing
// path (and one set of corner-case behaviors for malformed entries).
type siblingPair struct {
	role string
	id   string
}

// siblingPairs decodes pipeline_base.siblings into an ordered slice,
// silently dropping entries that don't match the expected
// `{role: string, id: string}` shape. The order matches the JSON array
// order — PostgreSQL JSONB arrays preserve insertion order, which is
// exactly why we picked an ordered array over an object map.
func siblingPairs(base map[string]any) []siblingPair {
	raw, ok := base["siblings"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]siblingPair, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := obj["role"].(string)
		id, _ := obj["id"].(string)
		if role == "" || id == "" {
			continue
		}
		out = append(out, siblingPair{role: role, id: id})
	}
	return out
}

// numericOrAny renders a JSON number cleanly when possible — float64
// values that are whole integers print as "5" rather than "5" with a
// floating-point fmt verb. Falls through to the default formatter for
// anything else (strings, bools, nested objects).
func numericOrAny(v any) any {
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return int64(f)
	}
	return v
}



// buildQuickCreatePrompt constructs a prompt for quick-create tasks. The
// user typed a single natural-language sentence in the create-issue modal;
// the agent's job is to translate it into one `multica issue create` CLI
// invocation, using its judgment to decide whether fetching referenced URLs
// would produce a better issue. No issue exists yet, so the agent must NOT
// call `multica issue get` or attempt to comment — there's nothing to read
// or reply to.
func buildQuickCreatePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a quick-create assistant for a Multica workspace.\n\n")
	b.WriteString("A user captured the following input via the quick-create modal. There is NO existing issue. Your job is to create a well-formed issue from this input with a single `multica issue create` command.\n\n")
	fmt.Fprintf(&b, "User input:\n> %s\n\n", task.QuickCreatePrompt)

	b.WriteString("Field rules:\n\n")

	// title
	b.WriteString("- **title**: required. A concise but semantically rich summary. If the input references external resources (PRs, issues, URLs), use your judgment on whether fetching the resource would produce a meaningfully better title — e.g. \"review PR #123\" → \"Review PR #123: Refactor auth module to OAuth2\". Strip filler words but preserve key semantic information.\n\n")

	// description — the core optimization
	b.WriteString("- **description**: The description is the executing agent's primary context. Aim for high fidelity — they should grasp the user's intent as if they had read the raw input themselves. Use a two-section structure:\n\n")
	b.WriteString("  1. **User request** — Faithfully restate what the user wants in their own words. Preserve specific names, identifiers, file paths, code snippets, and technical terms verbatim. Strip non-spec material before writing it (this is removal, not paraphrasing): verbal routing wrappers about creating the issue or routing it (e.g. \"create an issue\", \"分配给 X\", \"让 @X 处理\") and pure conversational fillers (e.g. \"对吧？\"). When in doubt, keep it.\n\n")
	b.WriteString("     CC exception: `multica issue create` has no `--subscriber` flag, and the platform auto-subscribes members whose `[@Name](mention://member/<uuid>)` link appears in the description. When the user wrote \"cc @Y\", strip the verbal \"cc\" wrapper from the User request body and append a final `CC: <mention link(s)>` line to the description so the cc routing still fires.\n\n")
	b.WriteString("  2. **Context** — include ONLY when the input cited external resources AND you successfully fetched them AND they produced verifiable facts worth recording. Summarize facts only (e.g. \"PR #45 changes auth to JWT\"), not interpretation or unsolicited reference implementations. If you have nothing factual to add, omit the section entirely — never use it as an apology log for resources you could not fetch.\n\n")
	b.WriteString("  Hard rules: never invent requirements, implementation details, or acceptance criteria the user did not express; never reduce multi-sentence input to a single vague sentence; never echo the title.\n\n")

	// priority
	b.WriteString("- **priority**: one of `urgent`, `high`, `medium`, `low`, or omit. Map P0/P1 → urgent/high; \"asap\" → urgent. If unspecified, omit.\n\n")

	// assignee
	b.WriteString("- **assignee**:\n")
	b.WriteString("    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace members --output json`, `multica agent list --output json`, and `multica squad list --output json` and find the matching entity by display name. Squads are first-class assignees too — a squad name (e.g. \"Super Human\") routes work to the squad leader, who then delegates. On a clean unambiguous match, prefer `--assignee-id <uuid>` using the `user_id` (member) or `id` (agent or squad) from that JSON — UUID matching is exact and robust to name collisions in workspaces with overlapping names. `--assignee <name>` (fuzzy) is acceptable as a fallback when names are unambiguous. On no match or ambiguous match, do NOT pass either flag — instead append a final line to the description: `Unrecognized assignee: X`.\n")
	b.WriteString("    - Treat bare @-routing as an assignee directive even when the user did not write the English word \"assign\". This includes Chinese imperatives like `让 @独立团 review 这个 PR`, `给 @X 处理`, or `交给 @X`; strip the leading `@`/`＠` before matching display names. Do not keep that routing wrapper or `@Name` in the description unless it is a true CC-style notification rather than ownership. If the matched entity is a squad, pass the squad's `id` as `--assignee-id`, not the leader agent's id.\n")
	agentID := ""
	agentName := ""
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
	}
	switch {
	case task.SquadID != "":
		// The user opened quick-create with a SQUAD selected. The task
		// runs on the squad's leader agent, but the squad is the expected
		// owner — assigning to the leader would mask the squad's
		// delegation flow. Always point the default at the squad UUID.
		if task.SquadName != "" {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD %q: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadName, task.SquadID)
		} else {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadID)
		}
	case agentID != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee-id %q` (your agent UUID). The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned. Use the UUID flag, not `--assignee <name>`, so the assignment is unambiguous even when other agents share part of your name.\n\n", agentID)
	case agentName != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee %q`. The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned.\n\n", agentName)
	default:
		b.WriteString("    - When the user did NOT name an assignee, default to YOURSELF (the picker agent): pass `--assignee-id <your agent UUID>` (preferred) or `--assignee <your agent name>`. Never leave the issue unassigned.\n\n")
	}

	// project — pinned by the modal when the user picked one, otherwise
	// omitted so the platform routes to the workspace default. Always pass
	// the UUID (never a name) so the issue lands in the right project even
	// when several share a title.
	if task.ProjectID != "" {
		if task.ProjectTitle != "" {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in project %q (the user picked it in the quick-create modal). Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID, task.ProjectTitle)
		} else {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in the project the user picked in the quick-create modal. Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID)
		}
	} else {
		b.WriteString("- **project**: omit. The platform will route the issue to the workspace default.\n")
	}
	b.WriteString("- **status**: omit (defaults to `todo`).\n")
	b.WriteString("- **attachments**: do NOT pass `--attachment`. The flag only accepts LOCAL file paths. Any image URL in the user input is already markdown — keep it inline in `--description` instead.\n\n")

	// output format
	b.WriteString("Output format:\n")
	b.WriteString("- Run exactly one `multica issue create --output json` invocation. Do not retry for any reason — even on non-zero exit. The issue may already exist; another attempt would create a duplicate.\n")
	b.WriteString("- Parse the JSON response to read the created issue's `identifier` (preferred) or `id` (fallback). Do not scrape human output and do not assume any workspace issue prefix such as `MUL-`; workspaces can use custom prefixes.\n")
	b.WriteString("- After success, print exactly one line: `Created <identifier-or-id>: <title>` and exit. No commentary, no follow-up tool calls.\n")
	b.WriteString("- Do NOT call `multica issue get` or `multica issue comment add` — there is no issue to query or comment on.\n")
	b.WriteString("- On CLI error or JSON parse error, exit with the error as the only output. The platform writes a failure notification automatically.\n")
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task, provider string) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)

	// Prefer the typed IssueEvent payload (migration 098) over raw comment
	// text. When both are present, the structured event wins: it is the
	// canonical, deduplicated representation of the reviewer's intent.
	if task.WakeupEventKind != "" {
		b.WriteString("[WAKEUP EVENT] A structured pipeline event has been recorded for this issue:\n\n")
		fmt.Fprintf(&b, "- event_kind: %s\n", task.WakeupEventKind)
		if task.WakeupEventFeedback != "" {
			fmt.Fprintf(&b, "- feedback: %s\n", task.WakeupEventFeedback)
		}
		// Kind-specific brief so agent knows exactly what to do without
		// having to re-parse the raw comment body.
		switch task.WakeupEventKind {
		case "review_approved":
			b.WriteString("\nA human reviewer approved your output. Proceed with the approve verdict flow.\n")
		case "review_denied":
			b.WriteString("\nA human reviewer denied your output. Review the feedback above and rework accordingly.\n")
		case "escalation_requested":
			b.WriteString("\nA human escalation has been requested. Review the feedback and decide how to proceed or wait for PM input.\n")
		}
		b.WriteString("\n")
	}

	if task.TriggerCommentContent != "" {
		authorLabel := "A user"
		if task.TriggerAuthorType == "agent" {
			name := task.TriggerAuthorName
			if name == "" {
				name = "another agent"
			}
			authorLabel = fmt.Sprintf("Another agent (%s)", name)
		}
		// Only show the raw comment if there's no structured event; when
		// there IS a structured event the raw comment is still shown as
		// supporting context — it's the human-authored body and reviewers
		// often add detail beyond the approve/deny header.
		if task.WakeupEventKind != "" {
			fmt.Fprintf(&b, "[COMMENT CONTEXT] Raw comment from %s:\n\n", strings.ToLower(authorLabel))
		} else {
			fmt.Fprintf(&b, "[NEW COMMENT] %s just left a new comment. Focus on THIS comment — do not confuse it with previous ones:\n\n", authorLabel)
		}
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
		if task.TriggerAuthorType == "agent" {
			b.WriteString("⚠️ The triggering comment was posted by another agent. Decide whether a reply is warranted. If you produced actual work this turn (investigated, fixed something, answered a real question), post the result as a normal reply — that is NOT a noise comment, and the standard rule that final results must be delivered via comment still applies. If the triggering comment was a pure acknowledgment, thanks, or sign-off AND you produced no work this turn, do NOT reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is the preferred way to end agent-to-agent threads. If you do reply, do not @mention the other agent as a sign-off (that re-triggers them and starts a loop).\n\n")
		}
		if task.Agent != nil && strings.Contains(task.Agent.Instructions, "## Squad Operating Protocol") {
			fmt.Fprintf(&b, "⚠️ **Squad leader no_action rule:** If you decide no action is needed, call `multica squad activity %s no_action --reason \"...\"` and EXIT. DO NOT post any comment — not even one that says \"no action needed\" or \"exiting silently\". The squad activity call records your decision; a comment is redundant noise.\n\n", task.IssueID)
		}
	}
	if pipeline := renderPipelinePrefix(task); pipeline != "" {
		b.WriteString(pipeline)
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then decide how to proceed.\n\n", task.IssueID)
	fmt.Fprintf(&b, "If you need comment history, `multica issue comment list %s --output json` returns all comments for the issue (server caps at 2000). Pass `--since <RFC3339>` to fetch only comments newer than a known cursor.\n\n", task.IssueID)
	b.WriteString(execenv.BuildCommentReplyInstructions(provider, task.IssueID, task.TriggerCommentID))
	// Dynamic counters go last so they don't shift the cache boundary.
	if dyn := renderPipelineDynamic(task); dyn != "" {
		b.WriteString(dyn)
	}
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	// List attachments by id + filename so the agent can fetch them via
	// the CLI. We deliberately do NOT inline the URL: chat attachments
	// live behind a signed CDN with a short TTL, so by the time the agent
	// has finished thinking the URL embedded in the markdown body may
	// have expired. `multica attachment download <id>` re-signs at click
	// time and is the only reliable path.
	if len(task.ChatMessageAttachments) > 0 {
		b.WriteString("\nAttachments on this message:\n")
		for _, a := range task.ChatMessageAttachments {
			if a.ContentType != "" {
				fmt.Fprintf(&b, "- id=%s filename=%q content_type=%s\n", a.ID, a.Filename, a.ContentType)
			} else {
				fmt.Fprintf(&b, "- id=%s filename=%q\n", a.ID, a.Filename)
			}
		}
		b.WriteString("Use `multica attachment download <id>` to fetch each file locally before referring to it.\n")
	}
	return b.String()
}

// buildAutopilotPrompt constructs a prompt for run_only autopilot tasks.
func buildAutopilotPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	b.WriteString("This task was triggered by an Autopilot in run-only mode. There is no assigned Multica issue for this run.\n\n")
	fmt.Fprintf(&b, "Autopilot run ID: %s\n", task.AutopilotRunID)
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Autopilot ID: %s\n", task.AutopilotID)
	}
	if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "Autopilot title: %s\n", task.AutopilotTitle)
	}
	if task.AutopilotSource != "" {
		fmt.Fprintf(&b, "Trigger source: %s\n", task.AutopilotSource)
	}
	if strings.TrimSpace(string(task.AutopilotTriggerPayload)) != "" {
		fmt.Fprintf(&b, "Trigger payload:\n%s\n", strings.TrimSpace(string(task.AutopilotTriggerPayload)))
	}
	b.WriteString("\nAutopilot instructions:\n")
	if strings.TrimSpace(task.AutopilotDescription) != "" {
		b.WriteString(task.AutopilotDescription)
		b.WriteString("\n\n")
	} else if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "%s\n\n", task.AutopilotTitle)
	} else {
		b.WriteString("No additional autopilot instructions were provided. Inspect the autopilot configuration before proceeding.\n\n")
	}
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Start by running `multica autopilot get %s --output json` if you need the full autopilot configuration, then complete the instructions above.\n", task.AutopilotID)
	} else {
		b.WriteString("Complete the instructions above.\n")
	}
	b.WriteString("Do not run `multica issue get`; this run does not have an issue ID.\n")
	return b.String()
}

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	devReportTestMemberEmail   = "dev-report-member@multica.ai"
	devReportTestOutsiderEmail = "dev-report-outsider@multica.ai"
	devReportForeignSlug       = "dev-report-foreign-tests"
)

// devReportRequest issues a request with an explicit token + workspace so the
// tests can exercise cross-workspace and role behaviour (authRequest is pinned
// to the fixture owner).
func devReportRequest(t *testing.T, method, path, token, workspaceID string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func devReportPublishBody(version, key, body string) map[string]any {
	return map[string]any{
		"schema_version":     1,
		"version":            version,
		"comparison_version": "3.33.0",
		"title":              "CC " + version + " 版本运营报告",
		"metric_version":     "historical-comment-inference/v1",
		"observation_as_of":  "2026-10-28T02:11:14Z",
		"body_markdown":      body,
		"publish_key":        key,
	}
}

func devReportTestVersion(t *testing.T) string {
	return "test-" + time.Now().UTC().Format("150405.000000000")
}

func closeBody(resp *http.Response) { io.Copy(io.Discard, resp.Body); resp.Body.Close() }

func TestDevReportPublishAndReadThroughRouter(t *testing.T) {
	version := devReportTestVersion(t)
	body := "# CC 运营报告\n\n| 指标 | 上版 | 本版 |\n|---|---|---|\n| **QA 逃逸** | 30 | 暂无 |\n"

	resp := devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
		devReportPublishBody(version, "key-"+version+"-1", body))
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("publish: expected 201, got %d: %s", resp.StatusCode, raw)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)
	if created["revision"].(float64) != 1 {
		t.Fatalf("first publish should be revision 1, got %v", created["revision"])
	}
	sum := sha256.Sum256([]byte(body))
	if created["body_sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("body_sha256 mismatch: %v", created["body_sha256"])
	}
	if _, exists := created["body_markdown"]; exists {
		t.Fatal("publish response should not echo the body")
	}

	// Detail returns the body byte-for-byte.
	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id, testToken, testWorkspaceID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}
	var detail map[string]any
	readJSON(t, resp, &detail)
	if detail["body_markdown"] != body {
		t.Fatalf("body changed in transit:\n want %q\n got  %q", body, detail["body_markdown"])
	}
	if detail["observation_as_of"] != "2026-10-28T02:11:14Z" {
		t.Fatalf("observation_as_of drifted: %v", detail["observation_as_of"])
	}

	// Markdown download is the same body.
	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id+"/markdown", testToken, testWorkspaceID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("download: expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("download content-type: %s", ct)
	}
	downloaded, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(downloaded) != body {
		t.Fatalf("download body mismatch")
	}

	// Idempotent replay returns the same revision with 200.
	resp = devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
		devReportPublishBody(version, "key-"+version+"-1", body))
	if resp.StatusCode != 200 {
		t.Fatalf("replay: expected 200, got %d", resp.StatusCode)
	}
	var replay map[string]any
	readJSON(t, resp, &replay)
	if replay["id"] != id || replay["revision"].(float64) != 1 {
		t.Fatalf("replay returned a different revision: %v", replay)
	}

	// Same key, different content is a conflict.
	resp = devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
		devReportPublishBody(version, "key-"+version+"-1", body+"\n篡改"))
	if resp.StatusCode != 409 {
		t.Fatalf("same key different body: expected 409, got %d", resp.StatusCode)
	}
	closeBody(resp)

	// A correction becomes revision 2; the old revision stays readable.
	corrected := body + "\n更正说明。\n"
	resp = devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
		devReportPublishBody(version, "key-"+version+"-2", corrected))
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("second revision: expected 201, got %d: %s", resp.StatusCode, raw)
	}
	var second map[string]any
	readJSON(t, resp, &second)
	if second["revision"].(float64) != 2 {
		t.Fatalf("expected revision 2, got %v", second["revision"])
	}

	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id, testToken, testWorkspaceID, nil)
	var oldDetail map[string]any
	readJSON(t, resp, &oldDetail)
	if oldDetail["body_markdown"] != body {
		t.Fatal("old revision body changed after a correction")
	}

	// List keeps only the highest revision per version and omits bodies.
	resp = devReportRequest(t, "GET", "/api/dev-reports?version="+version, testToken, testWorkspaceID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list: expected 200, got %d", resp.StatusCode)
	}
	var list struct {
		Reports []map[string]any `json:"reports"`
		Total   int64            `json:"total"`
	}
	readJSON(t, resp, &list)
	if list.Total != 1 || len(list.Reports) != 1 {
		t.Fatalf("expected exactly one version in list, got total=%d len=%d", list.Total, len(list.Reports))
	}
	if list.Reports[0]["revision"].(float64) != 2 {
		t.Fatalf("list should surface revision 2, got %v", list.Reports[0]["revision"])
	}
	if _, exists := list.Reports[0]["body_markdown"]; exists {
		t.Fatal("list must not include body_markdown")
	}

	// Revision history for the version, newest first.
	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id+"/revisions", testToken, testWorkspaceID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("revisions: expected 200, got %d", resp.StatusCode)
	}
	var revisions struct {
		Revisions []map[string]any `json:"revisions"`
		Total     int64            `json:"total"`
	}
	readJSON(t, resp, &revisions)
	if revisions.Total != 2 || len(revisions.Revisions) != 2 {
		t.Fatalf("expected 2 revisions, got total=%d len=%d", revisions.Total, len(revisions.Revisions))
	}
	if revisions.Revisions[0]["revision"].(float64) != 2 {
		t.Fatalf("revisions should be newest first, got %v", revisions.Revisions[0]["revision"])
	}
}

func TestDevReportPublicReadWithoutAuth(t *testing.T) {
	version := devReportTestVersion(t)

	resp := devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
		devReportPublishBody(version, "public-"+version, "# public report"))
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("publish: expected 201, got %d: %s", resp.StatusCode, raw)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)

	// Public read: no Authorization header at all.
	resp = devReportRequest(t, "GET", "/api/public/dev-reports?workspace_slug="+integrationTestWorkspaceSlug+"&version="+version, "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("public list: expected 200, got %d", resp.StatusCode)
	}
	var list struct {
		Reports []map[string]any `json:"reports"`
		Total   int64            `json:"total"`
	}
	readJSON(t, resp, &list)
	if list.Total != 1 || list.Reports[0]["id"] != id {
		t.Fatalf("public list missing report: %+v", list)
	}

	resp = devReportRequest(t, "GET", "/api/public/dev-reports/"+id+"?workspace_slug="+integrationTestWorkspaceSlug, "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("public detail: expected 200, got %d", resp.StatusCode)
	}
	var detail map[string]any
	readJSON(t, resp, &detail)
	if detail["body_markdown"] != "# public report" {
		t.Fatalf("public detail body mismatch: %v", detail["body_markdown"])
	}

	resp = devReportRequest(t, "GET", "/api/public/dev-reports/"+id+"/markdown?workspace_slug="+integrationTestWorkspaceSlug, "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("public markdown: expected 200, got %d", resp.StatusCode)
	}
	closeBody(resp)

	// Publishing must stay authenticated.
	resp = devReportRequest(t, "POST", "/api/dev-reports", "", testWorkspaceID,
		devReportPublishBody(version, "public-anon-publish-"+version, "# nope"))
	closeBody(resp)
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous publish: expected 401, got %d", resp.StatusCode)
	}

	// Public routes without a workspace identifier are rejected, not treated
	// as "all workspaces".
	resp = devReportRequest(t, "GET", "/api/public/dev-reports", "", "", nil)
	closeBody(resp)
	if resp.StatusCode != 400 {
		t.Fatalf("public list without workspace: expected 400, got %d", resp.StatusCode)
	}
}

func TestDevReportSurvivesProjectDeletion(t *testing.T) {
	version := devReportTestVersion(t)

	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, "Dev Report Project").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	payload := devReportPublishBody(version, "project-"+version, "# report with project")
	payload["project_id"] = projectID
	resp := devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID, payload)
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("publish: expected 201, got %d: %s", resp.StatusCode, raw)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)

	if _, err := testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id, testToken, testWorkspaceID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("report should survive project deletion, got %d", resp.StatusCode)
	}
	var detail map[string]any
	readJSON(t, resp, &detail)
	if detail["body_markdown"] != "# report with project" {
		t.Fatal("body changed after project deletion")
	}
	if detail["project_id"] != nil {
		t.Fatalf("project_id should be cleared, got %v", detail["project_id"])
	}
}

func TestDevReportValidationThroughRouter(t *testing.T) {
	version := devReportTestVersion(t)

	cases := []struct {
		name   string
		mutate func(map[string]any)
		status int
	}{
		{"missing schema", func(b map[string]any) { delete(b, "schema_version") }, 400},
		{"unknown schema", func(b map[string]any) { b["schema_version"] = 2 }, 400},
		{"empty body", func(b map[string]any) { b["body_markdown"] = "" }, 400},
		{"naive timestamp", func(b map[string]any) { b["observation_as_of"] = "2026-10-28T02:11:14" }, 400},
		{"bad project uuid", func(b map[string]any) { b["project_id"] = "not-a-uuid" }, 400},
		{"unknown project", func(b map[string]any) { b["project_id"] = "00000000-0000-0000-0000-000000000000" }, 400},
		{"missing publish key", func(b map[string]any) { delete(b, "publish_key") }, 400},
		{"missing title", func(b map[string]any) { b["title"] = "  " }, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := devReportPublishBody(version, fmt.Sprintf("invalid-%s-%s", tc.name, version), "# body")
			tc.mutate(payload)
			resp := devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID, payload)
			closeBody(resp)
			if resp.StatusCode != tc.status {
				t.Fatalf("expected %d, got %d", tc.status, resp.StatusCode)
			}
		})
	}

	// Oversized payload (limit is 1 MiB).
	huge := strings.Repeat("x", (1<<20)+1024)
	resp := devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
		devReportPublishBody(version, "oversized-"+version, huge))
	closeBody(resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: expected 413, got %d", resp.StatusCode)
	}

	// Malformed / unknown report ids.
	resp = devReportRequest(t, "GET", "/api/dev-reports/not-a-uuid", testToken, testWorkspaceID, nil)
	closeBody(resp)
	if resp.StatusCode != 400 {
		t.Fatalf("bad uuid: expected 400, got %d", resp.StatusCode)
	}
	resp = devReportRequest(t, "GET", "/api/dev-reports/00000000-0000-0000-0000-000000000001", testToken, testWorkspaceID, nil)
	closeBody(resp)
	if resp.StatusCode != 404 {
		t.Fatalf("missing report: expected 404, got %d", resp.StatusCode)
	}
}

func TestDevReportAuthorizationThroughRouter(t *testing.T) {
	ctx := context.Background()
	version := devReportTestVersion(t)

	memberToken := devReportCreateUserAndMember(t, ctx, devReportTestMemberEmail, testWorkspaceID, "member")
	outsiderToken := devReportCreateUserAndMember(t, ctx, devReportTestOutsiderEmail, "", "")
	foreignWorkspaceID := devReportCreateForeignWorkspace(t, ctx)

	// Publish with the fixture owner.
	resp := devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
		devReportPublishBody(version, "auth-"+version, "# report"))
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("owner publish: expected 201, got %d: %s", resp.StatusCode, raw)
	}
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)

	// Plain members can read but cannot publish.
	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id, memberToken, testWorkspaceID, nil)
	closeBody(resp)
	if resp.StatusCode != 200 {
		t.Fatalf("member read: expected 200, got %d", resp.StatusCode)
	}
	resp = devReportRequest(t, "POST", "/api/dev-reports", memberToken, testWorkspaceID,
		devReportPublishBody(version, "member-"+version, "# report"))
	closeBody(resp)
	if resp.StatusCode != 403 {
		t.Fatalf("member publish: expected 403, got %d", resp.StatusCode)
	}

	// Another workspace's report id never leaks through the CC header.
	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id, outsiderToken, foreignWorkspaceID, nil)
	closeBody(resp)
	if resp.StatusCode == 200 {
		t.Fatal("cross-workspace read must not return the report")
	}
	// Same report id with the owner's workspace but an outsider token.
	resp = devReportRequest(t, "GET", "/api/dev-reports/"+id, outsiderToken, testWorkspaceID, nil)
	closeBody(resp)
	if resp.StatusCode == 200 {
		t.Fatal("non-member read must not return the report")
	}

	// No credentials at all.
	resp = devReportRequest(t, "GET", "/api/dev-reports", "", testWorkspaceID, nil)
	closeBody(resp)
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous list: expected 401, got %d", resp.StatusCode)
	}
}

func TestDevReportConcurrentPublishAllocatesDistinctRevisions(t *testing.T) {
	version := devReportTestVersion(t)

	const workers = 4
	var wg sync.WaitGroup
	ids := make([]string, workers)
	revisions := make([]float64, workers)
	statuses := make([]int, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := devReportRequest(t, "POST", "/api/dev-reports", testToken, testWorkspaceID,
				devReportPublishBody(version, fmt.Sprintf("concurrent-%s-%d", version, i), fmt.Sprintf("# report %d", i)))
			defer closeBody(resp)
			statuses[i] = resp.StatusCode
			if resp.StatusCode != 201 {
				return
			}
			var payload map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				return
			}
			ids[i], _ = payload["id"].(string)
			revisions[i], _ = payload["revision"].(float64)
		}(i)
	}
	wg.Wait()

	seen := map[float64]bool{}
	for i := 0; i < workers; i++ {
		if statuses[i] != 201 {
			t.Fatalf("worker %d publish failed with %d", i, statuses[i])
		}
		if seen[revisions[i]] {
			t.Fatalf("duplicate revision %v", revisions[i])
		}
		seen[revisions[i]] = true
	}
	for want := 1.0; want <= workers; want++ {
		if !seen[want] {
			t.Fatalf("missing revision %v (got %v)", want, seen)
		}
	}
}

func devReportCreateUserAndMember(t *testing.T, ctx context.Context, email, workspaceID, role string) string {
	t.Helper()
	if _, err := testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email); err != nil {
		t.Fatalf("clean user: %v", err)
	}
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Dev Report Test User", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if workspaceID != "" && role != "" {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
		`, workspaceID, userID, role); err != nil {
			t.Fatalf("create member: %v", err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})
	token, err := generateTestJWT(userID, email, "Dev Report Test User")
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}
	return token
}

func devReportCreateForeignWorkspace(t *testing.T, ctx context.Context) string {
	t.Helper()
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, devReportForeignSlug)
	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description) VALUES ($1, $2, $3) RETURNING id
	`, "Dev Report Foreign", devReportForeignSlug, "Foreign workspace for isolation tests").Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, devReportForeignSlug)
	})
	return workspaceID
}

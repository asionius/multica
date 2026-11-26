package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const docStoreOtherEmail = "doc-store-other@multica.ai"

func docStoreRequest(t *testing.T, method, key, token, contentType string, body []byte) *http.Response {
	t.Helper()
	url := testServer.URL + "/api/doc-store"
	if strings.HasPrefix(key, "?") {
		url += key
	} else if key != "" {
		url += "/" + key
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestDocStoreRoundTrip(t *testing.T) {
	key := fmt.Sprintf("weekly/%d/report.md", time.Now().UnixNano())
	value := []byte("# weekly report numbers 1,2,3")

	resp := docStoreRequest(t, "PUT", key, testToken, "text/markdown", value)
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("put: expected 200, got %d: %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	resp = docStoreRequest(t, "GET", key, testToken, "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, value) {
		t.Fatalf("value mismatch: %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("content type: %s", ct)
	}

	// Overwrite is a plain SET.
	next := []byte("replaced")
	resp = docStoreRequest(t, "PUT", key, testToken, "text/plain", next)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("overwrite: expected 200, got %d", resp.StatusCode)
	}
	resp = docStoreRequest(t, "GET", key, testToken, "", nil)
	got, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, next) {
		t.Fatalf("overwrite not applied: %q", got)
	}

	// List with prefix.
	prefix := key[:strings.LastIndex(key, "/")+1]
	resp = docStoreRequest(t, "GET", "", testToken, "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = docStoreRequest(t, "GET", "?prefix="+prefix, testToken, "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list prefix: expected 200, got %d", resp.StatusCode)
	}
	var list struct {
		Keys  []map[string]any `json:"keys"`
		Total int64            `json:"total"`
	}
	readJSON(t, resp, &list)
	if list.Total != 1 || list.Keys[0]["key"] != key {
		t.Fatalf("list mismatch: %+v", list)
	}

	resp = docStoreRequest(t, "DELETE", key, testToken, "", nil)
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
	}
	resp = docStoreRequest(t, "GET", key, testToken, "", nil)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("get after delete: expected 404, got %d", resp.StatusCode)
	}
}

func TestDocStoreAccessControl(t *testing.T) {
	key := fmt.Sprintf("private/%d", time.Now().UnixNano())

	otherToken := docStoreCreateUser(t, context.Background(), docStoreOtherEmail)

	// The allow-listed fixture user can write; the second allow-listed user
	// is isolated by owner_user_id.
	resp := docStoreRequest(t, "PUT", key, testToken, "text/plain", []byte("secret"))
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("owner put: expected 200, got %d", resp.StatusCode)
	}
	resp = docStoreRequest(t, "GET", key, otherToken, "", nil)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("other user get: expected 404, got %d", resp.StatusCode)
	}

	// A non-allow-listed account is rejected before touching any row.
	outsider := docStoreCreateUser(t, context.Background(), "doc-store-outsider@multica.ai")
	resp = docStoreRequest(t, "PUT", key, outsider, "text/plain", []byte("nope"))
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("outsider put: expected 403, got %d", resp.StatusCode)
	}
	resp = docStoreRequest(t, "GET", "", outsider, "", nil)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("outsider list: expected 403, got %d", resp.StatusCode)
	}

	// Anonymous is 401.
	resp = docStoreRequest(t, "GET", "", "", "", nil)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous: expected 401, got %d", resp.StatusCode)
	}
}

func TestDocStoreValidation(t *testing.T) {
	// Invalid key: leading space.
	resp := docStoreRequest(t, "PUT", "%20bad", testToken, "text/plain", []byte("x"))
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("invalid key: expected 400, got %d", resp.StatusCode)
	}

	// Oversized body (>25 MiB).
	huge := bytes.Repeat([]byte("a"), (25<<20)+1024)
	resp = docStoreRequest(t, "PUT", "oversized/doc", testToken, "text/plain", huge)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: expected 413, got %d", resp.StatusCode)
	}

	// Missing document.
	resp = docStoreRequest(t, "GET", "missing/doc", testToken, "", nil)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("missing: expected 404, got %d", resp.StatusCode)
	}
}

func docStoreCreateUser(t *testing.T, ctx context.Context, email string) string {
	t.Helper()
	if _, err := testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email); err != nil {
		t.Fatalf("clean user: %v", err)
	}
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Doc Store Test User", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})
	token, err := generateTestJWT(userID, email, "Doc Store Test User")
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}
	return token
}

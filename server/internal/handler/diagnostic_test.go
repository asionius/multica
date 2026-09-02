package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDiagnosticsAdminEmailsFromEnv(t *testing.T) {
	t.Run("built-in default includes aikewang", func(t *testing.T) {
		got := DiagnosticsAdminEmailsFromEnv("")
		if len(got) != 1 || got[0] != "aikewang@tencent.com" {
			t.Fatalf("built-in default = %#v, want [aikewang@tencent.com]", got)
		}
	})

	t.Run("empty uses default list", func(t *testing.T) {
		prev := defaultDiagnosticsAdminEmails
		defaultDiagnosticsAdminEmails = []string{"a@tencent.com", "b@tencent.com"}
		t.Cleanup(func() { defaultDiagnosticsAdminEmails = prev })

		got := DiagnosticsAdminEmailsFromEnv("")
		if len(got) != 2 || got[0] != "a@tencent.com" || got[1] != "b@tencent.com" {
			t.Fatalf("empty env: got %#v", got)
		}
		got[0] = "mutated"
		if defaultDiagnosticsAdminEmails[0] != "a@tencent.com" {
			t.Fatal("FromEnv must copy the default list")
		}
	})

	t.Run("comma-separated overrides default", func(t *testing.T) {
		prev := defaultDiagnosticsAdminEmails
		defaultDiagnosticsAdminEmails = []string{"default@tencent.com"}
		t.Cleanup(func() { defaultDiagnosticsAdminEmails = prev })

		got := DiagnosticsAdminEmailsFromEnv(" one@tencent.com, two@tencent.com , ,three@tencent.com ")
		want := []string{"one@tencent.com", "two@tencent.com", "three@tencent.com"}
		if len(got) != len(want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		}
	})
}

func TestCreateSessionDiagnosticHappyPath(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	store := installMockStorage(t)

	archive := mustDiagnosticArchive(t, map[string][]byte{
		"index.json":          []byte(`{"id":"sess-1"}`),
		"messages/hello.json": []byte(`{"role":"user"}`),
	})
	req, rec := newDiagnosticUploadRequest(t, archive, map[string]string{
		"session_id":     "sess-1",
		"hostname":       "devbox",
		"plugin_version": "0.1.0",
		"note":           "repro",
	})
	testHandler.CreateSessionDiagnostic(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp CreateSessionDiagnosticResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if resp.ID == "" {
		t.Fatal("expected id")
	}
	if resp.ViewURL != "https://tmultica.woa.com/diagnostics/"+resp.ID {
		t.Fatalf("view_url = %q", resp.ViewURL)
	}
	if resp.DownloadURL != "https://tmultica.woa.com/api/diagnostics/"+resp.ID+"/download" {
		t.Fatalf("download_url = %q", resp.DownloadURL)
	}
	if _, ok := store.files["diagnostics/"+resp.ID+".tar.gz"]; !ok {
		t.Fatal("expected archive in storage")
	}
}

func TestCreateSessionDiagnosticEmptyFile(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)

	req, rec := newDiagnosticUploadRequest(t, []byte{}, map[string]string{"session_id": "sess-empty"})
	testHandler.CreateSessionDiagnostic(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateSessionDiagnosticRateLimit(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)

	archive := mustDiagnosticArchive(t, map[string][]byte{"index.json": []byte(`{}`)})
	for i := 0; i < diagnosticHourlyRateLimit; i++ {
		req, rec := newDiagnosticUploadRequest(t, archive, map[string]string{"session_id": "sess-rl"})
		testHandler.CreateSessionDiagnostic(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("iteration %d: status = %d, want 201; body=%s", i, rec.Code, rec.Body.String())
		}
	}
	req, rec := newDiagnosticUploadRequest(t, archive, map[string]string{"session_id": "sess-rl"})
	testHandler.CreateSessionDiagnostic(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListSessionDiagnosticsOwnerOnly(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)
	prevAdmins := testHandler.cfg.DiagnosticsAdminEmails
	testHandler.cfg.DiagnosticsAdminEmails = nil
	t.Cleanup(func() { testHandler.cfg.DiagnosticsAdminEmails = prevAdmins })

	own := uploadDiagnosticAs(t, testUserID, "own-session")
	otherID := createDiagnosticTestUser(t, "diag-other@multica.ai")
	theirs := uploadDiagnosticAs(t, otherID, "other-session")

	req := httptest.NewRequest("GET", "/api/diagnostics", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()
	testHandler.ListSessionDiagnostics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var list ListSessionDiagnosticsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Diagnostics) != 1 || list.Diagnostics[0].ID != own {
		t.Fatalf("non-admin list = %+v, want only %s", list.Diagnostics, own)
	}

	foreign := newDiagnosticIDRequest(t, "GET", "/api/diagnostics/"+theirs, theirs)
	foreign.Header.Set("X-User-ID", testUserID)
	rec = httptest.NewRecorder()
	testHandler.GetSessionDiagnostic(rec, foreign)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("get foreign: status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestMultipleDiagnosticsAdmins(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)
	otherID := createDiagnosticTestUser(t, "diag-second-admin@multica.ai")
	theirs := uploadDiagnosticAs(t, otherID, "multi-admin")

	prevAdmins := testHandler.cfg.DiagnosticsAdminEmails
	testHandler.cfg.DiagnosticsAdminEmails = []string{
		"someone-else@tencent.com",
		handlerTestEmail,
	}
	t.Cleanup(func() { testHandler.cfg.DiagnosticsAdminEmails = prevAdmins })

	req := newDiagnosticIDRequest(t, "GET", "/api/diagnostics/"+theirs, theirs)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()
	testHandler.GetSessionDiagnostic(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second listed admin: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetSessionDiagnosticAdminSeesAll(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)
	prevAdmins := testHandler.cfg.DiagnosticsAdminEmails
	testHandler.cfg.DiagnosticsAdminEmails = []string{handlerTestEmail}
	t.Cleanup(func() { testHandler.cfg.DiagnosticsAdminEmails = prevAdmins })

	otherID := createDiagnosticTestUser(t, "diag-admin-other@multica.ai")
	theirs := uploadDiagnosticAs(t, otherID, "admin-sees")

	req := httptest.NewRequest("GET", "/api/diagnostics", nil)
	req.Header.Set("X-User-ID", testUserID)
	rec := httptest.NewRecorder()
	testHandler.ListSessionDiagnostics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var list ListSessionDiagnosticsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, d := range list.Diagnostics {
		if d.ID == theirs {
			found = true
			if d.UploaderEmail != "diag-admin-other@multica.ai" {
				t.Fatalf("uploader_email = %q", d.UploaderEmail)
			}
		}
	}
	if !found {
		t.Fatalf("admin list missing %s: %+v", theirs, list.Diagnostics)
	}

	getReq := newDiagnosticIDRequest(t, "GET", "/api/diagnostics/"+theirs, theirs)
	getReq.Header.Set("X-User-ID", testUserID)
	rec = httptest.NewRecorder()
	testHandler.GetSessionDiagnostic(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin get: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateSessionDiagnosticRejectsPathTraversal(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)

	archive := mustRawTarGz(t, func(tw *tar.Writer) {
		writeTarFile(t, tw, "../etc/passwd", []byte("root"))
	})
	req, rec := newDiagnosticUploadRequest(t, archive, map[string]string{"session_id": "evil"})
	testHandler.CreateSessionDiagnostic(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateSessionDiagnosticRejectsSymlink(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)

	archive := mustRawTarGz(t, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{
			Name:     "messages/link",
			Typeflag: tar.TypeSymlink,
			Linkname: "../index.json",
		}); err != nil {
			t.Fatal(err)
		}
	})
	req, rec := newDiagnosticUploadRequest(t, archive, map[string]string{"session_id": "link"})
	testHandler.CreateSessionDiagnostic(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestDownloadAndPreviewSessionDiagnostic(t *testing.T) {
	clearDiagnosticsForTestUser(t)
	installMockStorage(t)

	index := []byte(`{"id":"preview-sess"}`)
	msg := []byte(`{"role":"assistant","text":"hi"}`)
	archive := mustDiagnosticArchive(t, map[string][]byte{
		"index.json":        index,
		"messages/001.json": msg,
	})
	upReq, upRec := newDiagnosticUploadRequest(t, archive, map[string]string{"session_id": "preview-sess"})
	testHandler.CreateSessionDiagnostic(upRec, upReq)
	if upRec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d; body=%s", upRec.Code, upRec.Body.String())
	}
	var created CreateSessionDiagnosticResponse
	if err := json.Unmarshal(upRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	dlReq := newDiagnosticIDRequest(t, "GET", "/api/diagnostics/"+created.ID+"/download", created.ID)
	dlRec := httptest.NewRecorder()
	testHandler.DownloadSessionDiagnostic(dlRec, dlReq)
	if dlRec.Code != http.StatusOK {
		t.Fatalf("download status = %d; body=%s", dlRec.Code, dlRec.Body.String())
	}
	if !bytes.Equal(dlRec.Body.Bytes(), archive) {
		t.Fatalf("download body mismatch: got %d bytes, want %d", dlRec.Body.Len(), len(archive))
	}

	previewReq := newDiagnosticIDRequest(t, "GET", "/api/diagnostics/"+created.ID+"/files?path=index.json", created.ID)
	previewRec := httptest.NewRecorder()
	testHandler.GetSessionDiagnosticFile(previewRec, previewReq)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview status = %d; body=%s", previewRec.Code, previewRec.Body.String())
	}
	if previewRec.Body.String() != string(index) {
		t.Fatalf("preview body = %q, want %q", previewRec.Body.String(), index)
	}
	if got := previewRec.Header().Get("X-Original-Content-Type"); got != "application/json" {
		t.Fatalf("X-Original-Content-Type = %q", got)
	}

	travReq := newDiagnosticIDRequest(t, "GET", "/api/diagnostics/"+created.ID+"/files?path=../etc/passwd", created.ID)
	travRec := httptest.NewRecorder()
	testHandler.GetSessionDiagnosticFile(travRec, travReq)
	if travRec.Code != http.StatusBadRequest {
		t.Fatalf("traversal status = %d, want 400; body=%s", travRec.Code, travRec.Body.String())
	}
}

func installMockStorage(t *testing.T) *mockStorage {
	t.Helper()
	store := &mockStorage{}
	orig := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = orig })
	return store
}

func clearDiagnosticsForTestUser(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM session_diagnostic WHERE user_id = $1`, parseUUID(testUserID)); err != nil {
		t.Fatalf("clear diagnostics: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM session_diagnostic WHERE user_id = $1`, parseUUID(testUserID))
	})
}

func createDiagnosticTestUser(t *testing.T, email string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id::text
	`, "Diagnostic Other", email).Scan(&id); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM session_diagnostic WHERE user_id = $1`, id)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, id)
	})
	return id
}

func uploadDiagnosticAs(t *testing.T, userID, sessionID string) string {
	t.Helper()
	archive := mustDiagnosticArchive(t, map[string][]byte{"index.json": []byte(`{"id":"` + sessionID + `"}`)})
	req, rec := newDiagnosticUploadRequest(t, archive, map[string]string{"session_id": sessionID})
	req.Header.Set("X-User-ID", userID)
	testHandler.CreateSessionDiagnostic(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload as %s: status = %d; body=%s", userID, rec.Code, rec.Body.String())
	}
	var resp CreateSessionDiagnosticResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.ID
}

func newDiagnosticUploadRequest(t *testing.T, archive []byte, fields map[string]string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "session.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/diagnostics", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	return req, httptest.NewRecorder()
}

func newDiagnosticIDRequest(t *testing.T, method, url, id string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("X-User-ID", testUserID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func mustDiagnosticArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	return mustRawTarGz(t, func(tw *tar.Writer) {
		for name, data := range files {
			writeTarFile(t, tw, name, data)
		}
	})
}

func mustRawTarGz(t *testing.T, write func(*tar.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write(tw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTarFile(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
}

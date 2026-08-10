package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func multipartRequest(t *testing.T, fieldName, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestUploadSavesTheFileAndReturnsItsPath(t *testing.T) {
	f := &fakeRunner{uploadPath: "/inbox/1699_dossier.pdf"}
	h := NewRouter(f, "")

	req := multipartRequest(t, "file", "dossier.pdf", []byte("%PDF-1.4 contenu réel"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Path != "/inbox/1699_dossier.pdf" {
		t.Errorf("path = %q", out.Path)
	}
	if len(f.uploaded) != 1 || f.uploaded[0].filename != "dossier.pdf" {
		t.Errorf("uploaded = %+v", f.uploaded)
	}
	if string(f.uploaded[0].content) != "%PDF-1.4 contenu réel" {
		t.Errorf("content = %q", f.uploaded[0].content)
	}
}

func TestUploadRejectsAMissingFileField(t *testing.T) {
	f := &fakeRunner{}
	h := NewRouter(f, "")

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestUploadSurfacesARunnerFailure(t *testing.T) {
	f := &fakeRunner{uploadErr: errors.New("disque plein")}
	h := NewRouter(f, "")

	req := multipartRequest(t, "file", "dossier.pdf", []byte("x"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestUploadRequiresAuthWhenATokenIsConfigured(t *testing.T) {
	f := &fakeRunner{}
	h := NewRouter(f, "secret")

	req := multipartRequest(t, "file", "dossier.pdf", []byte("x"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

type uploadCall struct {
	filename string
	content  []byte
}

func (f *fakeRunner) Upload(_ context.Context, filename string, r io.Reader) (string, error) {
	content, _ := io.ReadAll(r)
	f.uploaded = append(f.uploaded, uploadCall{filename: filename, content: content})
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	return f.uploadPath, nil
}

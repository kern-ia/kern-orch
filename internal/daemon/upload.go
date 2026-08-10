package daemon

import (
	"net/http"
)

// maxUploadBody caps an uploaded document — 25 MiB comfortably covers a scanned liasse of
// a few dozen pages; a larger body means something is wrong upstream (or abusive).
const maxUploadBody = 25 << 20

// handleUpload accepts one multipart-form document (field "file") and hands it to the
// Runner, which decides where it actually lands (see internal/cmd/serve.go's daemonRunner
// — a plain inbox directory, same shape as courtage-extraction's Telegram inbox). Returns
// the local path a caller then dispatches courtage-extraction with, exactly the same
// "text IS the document path" convention as the chat and Telegram already use — no new
// ingestion contract, one more real caller of the existing one.
func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
	if err := r.ParseMultipartForm(maxUploadBody); err != nil {
		writeError(w, http.StatusBadRequest, "malformed upload: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing \"file\" field")
		return
	}
	defer file.Close()

	path, err := s.runner.Upload(r.Context(), header.Filename, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

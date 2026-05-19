package controller

import (
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	aiservice "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/gorilla/mux"
)

// GeneratedFileController serves GET /api/v1/generated-files/{fileId}
type GeneratedFileController struct {
	svc aiservice.AiChatService
}

func NewGeneratedFileController(svc aiservice.AiChatService) *GeneratedFileController {
	return &GeneratedFileController{svc: svc}
}

func (c *GeneratedFileController) Download(w http.ResponseWriter, r *http.Request) {
	fileID := mux.Vars(r)["fileId"]
	token := r.URL.Query().Get("token")
	if token == "" {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusUnauthorized, Code: exception.AiChatTokenMissing, Message: exception.AiChatTokenMissingMsg})
		return
	}
	uid, fid, err := security.ValidateGeneratedFileToken(token)
	if err != nil {
		if security.IsTokenExpiredError(err) {
			utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusGone, Code: exception.AiChatTokenExpired, Message: exception.AiChatTokenExpiredMsg, Debug: err.Error()})
			return
		}
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusUnauthorized, Code: exception.AiChatTokenInvalid, Message: exception.AiChatTokenInvalidMsg, Debug: err.Error()})
		return
	}
	if fid != fileID {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusUnauthorized, Code: exception.AiChatTokenInvalid, Message: "Token not valid for this file"})
		return
	}

	f, err := c.svc.GetFileForUser(r.Context(), fileID, uid)
	if err != nil {
		utils.RespondWithError(w, "Get file", err)
		return
	}
	if f == nil {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusNotFound, Code: exception.AiChatNotFound, Message: "File not found"})
		return
	}

	// Open the file ourselves so we can pass an io.ReadSeeker to http.ServeContent.
	// http.ServeContent (unlike http.ServeFile) respects Content-Type/Content-Disposition
	// headers that are already set on the response writer.
	file, err := os.Open(f.StoragePath)
	if err != nil {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusNotFound, Code: exception.AiChatNotFound, Message: "File not found on disk", Debug: err.Error()})
		return
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil || st.IsDir() {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: http.StatusNotFound, Code: exception.AiChatNotFound, Message: "File not found on disk", Debug: errToString(err)})
		return
	}

	if f.MimeType != nil {
		w.Header().Set("Content-Type", *f.MimeType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+escapeFilename(f.Filename)+"\"")
	http.ServeContent(w, r, "", st.ModTime(), file)
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// escapeFilename strips control characters and double-quotes from a filename
// before embedding it in a Content-Disposition header value.
func escapeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if !utf8.ValidRune(r) || r < 0x20 || r == 0x7f {
			return -1
		}
		if r == '"' {
			return '\''
		}
		return r
	}, s)
	return s
}

package controller

import (
	"net/http"
	"os"
	"strings"

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
		utils.RespondWithCustomError(w, &exception.CustomError{Status: 401, Code: "APIHUB-AI-3002", Message: "Missing token query parameter"})
		return
	}
	uid, fid, err := security.ValidateGeneratedFileToken(token)
	if err != nil {
		if security.IsTokenExpiredError(err) {
			utils.RespondWithCustomError(w, &exception.CustomError{Status: 410, Code: "APIHUB-AI-4101", Message: "Token expired", Debug: err.Error()})
			return
		}
		utils.RespondWithCustomError(w, &exception.CustomError{Status: 401, Code: "APIHUB-AI-3002", Message: "Invalid token", Debug: err.Error()})
		return
	}
	if fid != fileID {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: 401, Code: "APIHUB-AI-3002", Message: "Token not valid for this file"})
		return
	}

	f, err := c.svc.GetFileForUser(r.Context(), fileID, uid)
	if err != nil {
		utils.RespondWithError(w, "Get file", err)
		return
	}
	if f == nil {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: 404, Code: "APIHUB-AI-3002", Message: "File not found"})
		return
	}
	st, err := os.Stat(f.StoragePath)
	if err != nil || st.IsDir() {
		utils.RespondWithCustomError(w, &exception.CustomError{Status: 404, Code: "APIHUB-AI-3002", Message: "File not found on disk", Debug: errToString(err)})
		return
	}
	if f.MimeType != nil {
		w.Header().Set("Content-Type", *f.MimeType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	disp := "attachment; filename=\"" + escapeFilename(f.Filename) + "\""
	w.Header().Set("Content-Disposition", disp)
	http.ServeFile(w, r, f.StoragePath)
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func escapeFilename(s string) string {
	// minimal escaping
	s = strings.ReplaceAll(s, "\"", "'")
	return s
}

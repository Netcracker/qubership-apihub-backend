package security

import (
	"fmt"
	"strings"
	"time"

	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
)

const (
	// GeneratedFileDownloadTokenType is the TokenTypeExt value for short-lived file download JWTs
	GeneratedFileDownloadTokenType = "generated-file-download"
	fileIDExt                      = "fileId"
)

// MintGeneratedFileToken issues a short-lived RS256 JWT that authorizes a single file download
func MintGeneratedFileToken(userID, fileID string, ttl time.Duration) (string, error) {
	if defaultJWTValidator == nil {
		return "", fmt.Errorf("security not initialized: JWT validator is nil")
	}
	user := auth.NewUserInfo("", userID, nil, auth.Extensions{})
	ext := user.GetExtensions()
	ext.Set(TokenTypeExt, GeneratedFileDownloadTokenType)
	ext.Set(fileIDExt, fileID)
	return jwt.IssueAccessToken(user, keeper, jwt.SetExpDuration(ttl))
}

// ValidateGeneratedFileToken returns user and file id if the token is valid for file download
func ValidateGeneratedFileToken(token string) (userID, fileID string, err error) {
	if defaultJWTValidator == nil {
		return "", "", fmt.Errorf("security not initialized")
	}
	info, exp, err := defaultJWTValidator.ValidateToken(token, GeneratedFileDownloadTokenType)
	if err != nil {
		return "", "", err
	}
	_ = exp
	fid := info.GetExtensions().Get(fileIDExt)
	if fid == "" {
		return "", "", fmt.Errorf("missing fileId claim")
	}
	return info.GetID(), fid, nil
}

// IsTokenExpiredError is a best-effort check for HTTP 410 on expired file-download JWTs.
// Matches "token is expired", "token expired", "exp claim", etc. but not unrelated words
// that happen to contain "exp" (e.g. "expected", "expression").
func IsTokenExpiredError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "expired") ||
		strings.Contains(s, "token is exp") ||
		strings.Contains(s, "exp claim")
}

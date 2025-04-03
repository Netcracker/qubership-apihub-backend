package view

const SessionCookieName = "apihub-session"

type SessionCookie struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

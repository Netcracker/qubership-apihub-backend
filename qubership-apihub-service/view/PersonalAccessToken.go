package view

import (
	"time"
)

type PersonalAccessTokenCreateRequest struct {
	Name            string `json:"name" validate:"required"`
	DaysUntilExpiry int    `json:"daysUntilExpiry" validate:"required"`
}

type PersonalAccessTokenCreateResponse struct {
	PersonalAccessTokenItem
	Token string `json:"token"`
}

type PersonaAccessTokenStatus string

const PersonaAccessTokenActive PersonaAccessTokenStatus = "active"
const PersonaAccessTokenExpired PersonaAccessTokenStatus = "expired"

type PersonalAccessTokenItem struct {
	Id        string                   `json:"id"`
	Name      string                   `json:"name"`
	ExpiresAt *time.Time               `json:"expiresAt"`
	CreatedAt time.Time                `json:"createdAt"`
	Status    PersonaAccessTokenStatus `json:"status"`
}

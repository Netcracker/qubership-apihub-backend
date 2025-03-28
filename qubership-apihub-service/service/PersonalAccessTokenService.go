// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"fmt"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/context"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/crypto"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/google/uuid"
	"net/http"
	"time"
)

type PersonalAccessTokenService interface {
	CreatePAT(ctx context.SecurityContext, req view.PersonalAccessTokenCreateRequest) (*view.PersonalAccessTokenCreateResponse, error)
	DeletePAT(ctx context.SecurityContext, id string) error
	GetPATByToken(pat string) (*view.PersonalAccessTokenItem, *view.User, string, error)
	ListPATs(userId string) ([]view.PersonalAccessTokenItem, error)
}

func NewPersonalAccessTokenService(repo repository.PersonalAccessTokenRepository, userService UserService, roleService RoleService) PersonalAccessTokenService {
	return personalAccessTokenServiceImpl{repo: repo, userService: userService, roleService: roleService}
}

type personalAccessTokenServiceImpl struct {
	repo        repository.PersonalAccessTokenRepository
	userService UserService
	roleService RoleService
}

const ActivePatPerUserLimit = 100

func (p personalAccessTokenServiceImpl) CreatePAT(ctx context.SecurityContext, req view.PersonalAccessTokenCreateRequest) (*view.PersonalAccessTokenCreateResponse, error) {
	//TODO: The validations are not thread-safe, but probably it's ok for now

	count, err := p.repo.CountActiveTokens(ctx.GetUserId())
	if err != nil {
		return nil, fmt.Errorf("failed to check token limit: %w", err)
	}
	if count >= ActivePatPerUserLimit {
		return nil, exception.CustomError{
			Status:  http.StatusConflict,
			Code:    exception.PersonalAccessTokenLimitExceeded,
			Message: exception.PersonalAccessTokenLimitExceededMsg,
			Params:  map[string]interface{}{"limit": ActivePatPerUserLimit},
		}
	}

	free, err := p.repo.CheckNameIsFree(ctx.GetUserId(), req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check token name availability: %w", err)
	}
	if !free {
		return nil, exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.PersonalAccessTokenNameIsUsed,
			Message: exception.PersonalAccessTokenNameIsUsedMsg,
			Params:  map[string]interface{}{"name": req.Name},
		}
	}

	pat := crypto.CreateRandomHash()
	tokenHash := crypto.CreateSHA256Hash([]byte(pat))

	expiresAt, err := calculateExpiresAt(req.DaysUntilExpiry)
	if err != nil {
		return nil, err
	}

	ent := entity.PersonaAccessTokenEntity{
		Id:        uuid.New().String(),
		UserId:    ctx.GetUserId(),
		TokenHash: tokenHash,
		Name:      req.Name,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
		DeletedAt: time.Time{},
	}

	err = p.repo.CreatePAT(ent)
	if err != nil {
		return nil, err
	}

	resp := &view.PersonalAccessTokenCreateResponse{
		PersonalAccessTokenItem: entity.MakePersonaAccessTokenView(ent),
		Token:                   pat,
	}
	return resp, nil
}

func calculateExpiresAt(daysUntilExpiry int) (time.Time, error) {
	if daysUntilExpiry == -1 {
		return time.Time{}, nil
	}

	if daysUntilExpiry < -1 || daysUntilExpiry == 0 {
		return time.Time{}, exception.CustomError{
			Status:  http.StatusBadRequest,
			Code:    exception.PersonalAccessTokenIncorrectExpiry,
			Message: exception.PersonalAccessTokenIncorrectExpiryMsg,
			Params:  map[string]interface{}{"param": "daysUntilExpiry"},
		}
	}

	return time.Now().Add(time.Duration(daysUntilExpiry) * 24 * time.Hour), nil
}

func (p personalAccessTokenServiceImpl) DeletePAT(ctx context.SecurityContext, id string) error {
	pat, err := p.repo.GetPAT(id, ctx.GetUserId())
	if err != nil {
		return fmt.Errorf("failed to get PAT: %s", err)
	}
	if pat == nil {
		return exception.CustomError{
			Status:  http.StatusNotFound,
			Code:    exception.PersonalAccessTokenNotFound,
			Message: exception.PersonalAccessTokenNotFoundMsg,
			Params:  map[string]interface{}{"id": id},
		}
	}
	return p.repo.DeletePAT(pat.Id, ctx.GetUserId())
}

func (p personalAccessTokenServiceImpl) GetPATByToken(pat string) (*view.PersonalAccessTokenItem, *view.User, string, error) {
	tokenHash := crypto.CreateSHA256Hash([]byte(pat))

	//TODO: some optimization wanted: this auth method is using 3 DB calls: get pat, get user, get system role

	ent, err := p.repo.GetPATByHash(tokenHash)
	if err != nil {
		return nil, nil, "", err
	}
	if ent == nil {
		return nil, nil, "", nil
	}
	result := entity.MakePersonaAccessTokenView(*ent)

	user, err := p.userService.GetUserFromDB(ent.UserId)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get user for pat: %s", err)
	}

	systemRole, err := p.roleService.GetUserSystemRole(user.Id)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get user system role for pat: %s", err)
	}
	return &result, user, systemRole, nil
}

func (p personalAccessTokenServiceImpl) ListPATs(userId string) ([]view.PersonalAccessTokenItem, error) {
	var pats []entity.PersonaAccessTokenEntity

	pats, err := p.repo.ListPATs(userId)
	if err != nil {
		return nil, err
	}
	result := make([]view.PersonalAccessTokenItem, 0, len(pats))
	for _, pat := range pats {
		v := entity.MakePersonaAccessTokenView(pat)
		result = append(result, v)
	}

	return result, nil
}

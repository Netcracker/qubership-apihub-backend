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

package repository

import (
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
)

type ApihubApiKeyRepository interface {
	SaveApiKey(apihubApiKeyEntity *entity.ApihubApiKeyEntity) error
	RevokeApiKey(id string, userId string) error
	GetPackageApiKeys(packageId string) ([]entity.ApihubApiKeyUserEntity, error)
	GetApiKeyByHash(apiKeyHash string) (*entity.ApihubApiKeyEntity, error)
	GetPackageApiKey(apiKeyId string, packageId string) (*entity.ApihubApiKeyUserEntity, error)
	GetApiKey(apiKeyId string) (*entity.ApihubApiKeyEntity, error)
}

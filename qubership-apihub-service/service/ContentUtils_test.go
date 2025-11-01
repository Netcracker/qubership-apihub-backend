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
	"strings"
	"testing"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/stretchr/testify/assert"
)

func TestSplit(t *testing.T) {
	var fileIds []string
	fileIds = append(fileIds, "fileName.json")
	fileIds = append(fileIds, "/fileName.json")
	fileIds = append(fileIds, "./fileName.json")
	fileIds = append(fileIds, "/./fileName.json")
	fileIds = append(fileIds, "dir/fileName.json")
	fileIds = append(fileIds, "dir/./fileName.json")
	fileIds = append(fileIds, ".dir/./fileName.json")
	fileIds = append(fileIds, "dir/../fileName.json")
	fileIds = append(fileIds, "./dir/../fileName.json")

	for _, fileId := range fileIds {
		path, name := utils.SplitFileId(fileId)
		if path == "." {
			t.Error("File Path after split can't equal to '.'")
		}
		if strings.HasPrefix(path, "/") {
			t.Error("File Path after split can't start from '/'")
		}

		if strings.Contains(name, "/") {
			t.Error("File Name after split can't contain '/'")
		}

		if strings.HasPrefix(path, "../") || strings.Contains(path, "/../") || strings.HasSuffix(path, "/..") {
			t.Error("File Path after split can't contain '..' directories")
		}

		if strings.Contains(path, "//") {
			t.Error("File Path after split can't contain '//'")
		}
	}
}

func TestCheckAvailability(t *testing.T) {
	folders := make(map[string]bool)
	folders["2021.4/worklog/"] = true
	folders["2021.4/gsmtmf/"] = true
	folders["2020.4/acmbi/"] = true
	folders["2020.4/cmp/tmf621/"] = true
	folders["apihub-config/"] = true
	folders["newfolder/"] = true
	folders["other/"] = true
	folders["/"] = true

	files := make(map[string]bool)
	files["2021.4/worklog/worklog.md"] = true
	files["2021.4/gsmtmf/gsmtmf.md"] = true
	files["2020.4/acmbi/acmbi.md"] = true
	files["2020.4/cmp/tmf621/tmf.md"] = true
	files["apihub-config/config.md"] = true
	files["newfolder/new.md"] = true
	files["other/other.md"] = true
	files["README.md"] = true

	assert.Error(t, checkAvailability("README.md", files, folders))
	assert.Error(t, checkAvailability("README.md/qwerty.md", files, folders))
	assert.Error(t, checkAvailability("other/other.md/qwerty.md", files, folders)) //gitlab allows this but it deletes 'other.md' file
	assert.Error(t, checkAvailability("2021.4", files, folders))

	assert.NoError(t, checkAvailability("2021.4/qwerty.md", files, folders))
	assert.NoError(t, checkAvailability("2021.4/worklog/qwerty.md", files, folders))
	assert.NoError(t, checkAvailability("2021.5", files, folders))
	assert.NoError(t, checkAvailability("readme.md", files, folders))
	assert.NoError(t, checkAvailability("readme.md/qwerty.md", files, folders)) //gitlab allows this
}

package controller

import (
	"net/http"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/secctx"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	log "github.com/sirupsen/logrus"
)

type MinioStorageController interface {
	DownloadFilesFromMinioToDatabase(w http.ResponseWriter, r *http.Request)
}

func NewMinioStorageController(minioCreds *view.MinioStorageCreds, minioStorageService service.MinioStorageService) MinioStorageController {
	return &minioStorageControllerImpl{
		minioStorageService: minioStorageService,
		minioCreds:          minioCreds,
	}
}

type minioStorageControllerImpl struct {
	minioStorageService service.MinioStorageService
	minioCreds          *view.MinioStorageCreds
}

func (m minioStorageControllerImpl) DownloadFilesFromMinioToDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := secctx.MakeUserContext(r)
	sufficientPrivileges := secctx.IsSysadm(ctx)
	if !sufficientPrivileges {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusForbidden,
			Code:    exception.InsufficientPrivileges,
			Message: exception.InsufficientPrivilegesMsg,
		})
		return
	}
	if !m.minioCreds.IsActive {
		utils.RespondWithCustomError(w, &exception.CustomError{
			Status:  http.StatusMethodNotAllowed,
			Message: "Minio integration is inactive. Please check envs for configuration"})
		return
	}
	err := m.minioStorageService.DownloadFilesFromBucketToDatabase()
	if err != nil {
		log.Error("Failed to download data from minio: ", err.Error())
		if customError, ok := err.(*exception.CustomError); ok {
			utils.RespondWithCustomError(w, customError)
		} else {
			utils.RespondWithCustomError(w, &exception.CustomError{
				Status:  http.StatusInternalServerError,
				Message: "Failed to download data from minio",
				Debug:   err.Error()})
		}
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

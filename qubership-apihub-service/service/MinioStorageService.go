package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	log "github.com/sirupsen/logrus"
)

type MinioStorageService interface {
	UploadFilesToBucket() error
	GetFile(ctx context.Context, tableName, entityId string) ([]byte, error)
	UploadFile(ctx context.Context, tableName, entityId string, content []byte) error
	RemoveFiles(ctx context.Context, tableName string, entityIds []string) error
	DownloadFilesFromBucketToDatabase() error
}

func NewMinioStorageService(buildRepository repository.BuildResultRepository, publishRepo repository.PublishedRepository, creds *view.MinioStorageCreds) MinioStorageService {
	return &minioStorageServiceImpl{
		buildRepository: buildRepository,
		minioClient:     createMinioClient(creds),
		publishRepo:     publishRepo,
		creds:           creds,
	}
}

type minioStorageServiceImpl struct {
	buildRepository repository.BuildResultRepository
	minioClient     *minioClient
	publishRepo     repository.PublishedRepository
	creds           *view.MinioStorageCreds
}

type minioClient struct {
	client *minio.Client
	error  error
}

// minioMigrationTimeout bounds a single S3<->DB data-migration run, including all of its workers, as a safety net.
const minioMigrationTimeout = 360 * time.Minute

// todo add more logs for ex - [15 / 100] entities were stored to database....
func (m minioStorageServiceImpl) DownloadFilesFromBucketToDatabase() error {
	utils.SafeAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), minioMigrationTimeout)
		defer cancel()
		if err := m.migrateBucketToDatabase(ctx); err != nil {
			log.Errorf("MINIO. Migration from bucket to database finished with errors: %s", err.Error())
			return
		}
		log.Info("MINIO. Migration from bucket to database finished successfully")
	})
	return nil
}

func (m minioStorageServiceImpl) migrateBucketToDatabase(ctx context.Context) error {
	buildResultFileKeys := make([]string, 0)
	publishedSourceArchiveFileKeys := make([]string, 0)
	foldersChan := m.minioClient.client.ListObjects(ctx, m.creds.BucketName, minio.ListObjectsOptions{})
	for folder := range foldersChan {
		if folder.Err != nil {
			return fmt.Errorf("failed to list minio folders: %w", folder.Err)
		}
		switch folder.Key {
		case fmt.Sprintf("%s/", view.BUILD_RESULT_TABLE):
			for buildResult := range m.minioClient.client.ListObjects(ctx, m.creds.BucketName, minio.ListObjectsOptions{Prefix: folder.Key}) {
				if buildResult.Err != nil {
					return fmt.Errorf("failed to list '%s' files: %w", folder.Key, buildResult.Err)
				}
				buildResultFileKeys = append(buildResultFileKeys, buildResult.Key)
			}
		case fmt.Sprintf("%s/", view.PUBLISHED_SOURCES_ARCHIVES_TABLE):
			for publishedSourceArchive := range m.minioClient.client.ListObjects(ctx, m.creds.BucketName, minio.ListObjectsOptions{Prefix: folder.Key}) {
				if publishedSourceArchive.Err != nil {
					return fmt.Errorf("failed to list '%s' files: %w", folder.Key, publishedSourceArchive.Err)
				}
				publishedSourceArchiveFileKeys = append(publishedSourceArchiveFileKeys, publishedSourceArchive.Key)
			}
		}
	}

	log.Infof("MINIO. %d files were found", len(buildResultFileKeys)+len(publishedSourceArchiveFileKeys))

	var workers sync.WaitGroup
	workerErrors := make([]error, 2)

	if len(buildResultFileKeys) > 0 {
		workers.Add(1)
		utils.SafeAsync(func() {
			defer workers.Done()
			workerErrors[0] = utils.SafeSync(func() error {
				migrationErrors := make([]error, 0)
				entitiesCount := 0
				for _, key := range buildResultFileKeys {
					if err := ctx.Err(); err != nil {
						migrationErrors = append(migrationErrors, fmt.Errorf("build_result migration interrupted: %w", err))
						break
					}
					buildId := getEntityId(fmt.Sprintf("%s/", view.BUILD_RESULT_TABLE), key)
					if buildId == "" {
						migrationErrors = append(migrationErrors, fmt.Errorf("unsupported file key format. folder - '%s/', file - '%s'", view.BUILD_RESULT_TABLE, key))
						continue
					}
					data, err := m.getFile(ctx, key)
					if err != nil {
						migrationErrors = append(migrationErrors, fmt.Errorf("failed to get file from minio by key '%s': %w", key, err))
						continue
					}
					err = m.buildRepository.StoreBuildResult(ctx, entity.BuildResultEntity{BuildId: buildId, Data: data})
					if err != nil {
						migrationErrors = append(migrationErrors, fmt.Errorf("StoreBuildResults() produce error: %w", err))
						break
					}
					entitiesCount++
				}
				log.Infof("%d build_result entities were stored from minio to database", entitiesCount)
				return errors.Join(migrationErrors...)
			})
		})
	}

	if len(publishedSourceArchiveFileKeys) > 0 {
		workers.Add(1)
		utils.SafeAsync(func() {
			defer workers.Done()
			workerErrors[1] = utils.SafeSync(func() error {
				migrationErrors := make([]error, 0)
				entitiesCount := 0
				for _, key := range publishedSourceArchiveFileKeys {
					if err := ctx.Err(); err != nil {
						migrationErrors = append(migrationErrors, fmt.Errorf("published_sources_archives migration interrupted: %w", err))
						break
					}
					checksum := getEntityId(fmt.Sprintf("%s/", view.PUBLISHED_SOURCES_ARCHIVES_TABLE), key)
					if checksum == "" {
						migrationErrors = append(migrationErrors, fmt.Errorf("unsupported file key format. folder - '%s/', file - '%s'", view.PUBLISHED_SOURCES_ARCHIVES_TABLE, key))
						continue
					}
					data, err := m.getFile(ctx, key)
					if err != nil {
						migrationErrors = append(migrationErrors, fmt.Errorf("failed to get file from minio by key '%s': %w", key, err))
						continue
					}
					err = m.publishRepo.SavePublishedSourcesArchive(ctx, &entity.PublishedSrcArchiveEntity{Checksum: checksum, Data: data})
					if err != nil {
						migrationErrors = append(migrationErrors, fmt.Errorf("SavePublishedSourcesArchives() produce error: %w", err))
						break
					}
					entitiesCount++
				}
				log.Infof("%d published_sources_archives entities were stored from minio to database", entitiesCount)
				return errors.Join(migrationErrors...)
			})
		})
	}

	workers.Wait()
	return errors.Join(workerErrors...)
}

func (m minioStorageServiceImpl) UploadFilesToBucket() error {
	ctx, cancel := context.WithTimeout(context.Background(), minioMigrationTimeout)
	defer cancel()
	err := m.createBucketIfNotExists(ctx)
	if err != nil {
		return err
	}

	log.Info("Uploading files to MINIO")
	var workers sync.WaitGroup
	workerErrors := make([]error, 2)

	workers.Add(1)
	utils.SafeAsync(func() {
		defer workers.Done()
		workerErrors[0] = utils.SafeSync(func() error {
			uploadErrors := make([]error, 0)
			uploadedIds, err := m.uploadBuildResults(ctx)
			if err != nil {
				uploadErrors = append(uploadErrors, fmt.Errorf("uploadBuildResults produces an error: %w", err))
			}
			log.Info("Build results were uploaded to MINIO")

			if len(uploadedIds) > 0 {
				err = m.buildRepository.DeleteBuildResults(ctx, uploadedIds)
				if err != nil {
					uploadErrors = append(uploadErrors, fmt.Errorf("DeleteBuildResults produces an error: %w", err))
				}
				log.Info("Build results were deleted from database")
			}
			return errors.Join(uploadErrors...)
		})
	})
	if !m.creds.IsOnlyForBuildResult {
		workers.Add(1)
		utils.SafeAsync(func() {
			defer workers.Done()
			workerErrors[1] = utils.SafeSync(func() error {
				uploadErrors := make([]error, 0)
				uploadedChecksums, err := m.uploadPublishedSourcesArchives(ctx)
				if err != nil {
					uploadErrors = append(uploadErrors, fmt.Errorf("uploadPublishedSourcesArchives produces an error: %w", err))
				}
				log.Info("Published source archives were uploaded to MINIO")

				if len(uploadedChecksums) > 0 {
					err = m.publishRepo.DeletePublishedSourcesArchives(ctx, uploadedChecksums)
					if err != nil {
						uploadErrors = append(uploadErrors, fmt.Errorf("DeletePublishedSourcesArchives produces an error: %w", err))
					}
					log.Info("Published source archives were deleted from database")
				}
				return errors.Join(uploadErrors...)
			})
		})
	}

	workers.Wait()
	return errors.Join(workerErrors...)
}

func (m minioStorageServiceImpl) createBucketIfNotExists(ctx context.Context) error {
	exists, err := bucketExists(ctx, m.minioClient.client, m.creds.BucketName)
	if err != nil {
		return err
	}
	if exists {
		log.Infof("Minio bucket - %s exists", m.creds.BucketName)
	} else {
		err = m.minioClient.client.MakeBucket(ctx, m.creds.BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return err
		}
		exists, err = bucketExists(ctx, m.minioClient.client, m.creds.BucketName)
		if err != nil {
			return err
		}
		if exists {
			log.Infof("Minio bucket - %s was created", m.creds.BucketName)
		}
	}
	return nil
}

func createMinioClient(creds *view.MinioStorageCreds) *minioClient {
	client := new(minioClient)
	var err error
	tr, err := minio.DefaultTransport(true)
	if err != nil {
		log.Warnf("error creating the minio connection: error creating the default transport layer: %v", err)
		client.error = err
		return client
	}

	// Decode custom certificate if provided
	var decodedCert []byte
	if creds.Crt != "" {
		decodedCert, err = base64.StdEncoding.DecodeString(creds.Crt)
		if err != nil {
			log.Warn(err.Error())
			client.error = err
			return client
		}
	}

	tlsConfig, err := utils.BuildSecureTLSConfig(decodedCert)
	if err != nil {
		log.Warn(err.Error())
		client.error = err
		return client
	}
	tr.TLSClientConfig = tlsConfig

	minioClient, err := minio.New(creds.Endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(creds.AccessKeyId, creds.SecretAccessKey, ""),
		Secure:    true,
		Transport: tr,
	})
	if err != nil {
		if strings.Contains(err.Error(), "endpoint") {
			err = errors.New("invalid storage URL")
		}
		log.Warn(err.Error())
		client.error = err
		return client
	}
	log.Infof("MINIO instance initialized")
	client.client = minioClient
	return client
}

func (m minioStorageServiceImpl) uploadBuildResults(ctx context.Context) ([]string, error) {
	offset := 0
	ids := make([]string, 0)
	var buildResult *entity.BuildResultEntity
	var err error
	for {
		buildResult, err = m.buildRepository.GetBuildResultWithOffset(ctx, offset)
		if err != nil {
			log.Infof("%d build_results were ulpoaded to minio storage, until got error", offset)
			break
		}
		if buildResult == nil {
			log.Infof("%d build_results were ulpoaded to minio storage, until buildResult is null", offset)
			break
		}
		err = m.putObject(ctx, buildFileName(view.BUILD_RESULT_TABLE, buildResult.BuildId), buildResult.Data)
		if err != nil {
			log.Infof("%d build_results were ulpoaded to minio storage, until got error", offset)
			break
		}
		ids = append(ids, buildResult.BuildId)
		offset++
	}
	return ids, err
}

// file name table_name + checksum
func (m minioStorageServiceImpl) uploadPublishedSourcesArchives(ctx context.Context) ([]string, error) {
	offset := 0
	checksums := make([]string, 0)
	var publishedSourceArchive *entity.PublishedSrcArchiveEntity
	var err error
	for {
		publishedSourceArchive, err = m.publishRepo.GetPublishedSourcesArchives(ctx, offset)
		if err != nil {
			log.Infof("%d published_sources_archives were uploaded to minio storage, before error was received", offset)
			break
		}
		if publishedSourceArchive == nil {
			log.Infof("%d published_sources_archives were uploaded to minio storage, before publishedSourceArchive became null", offset)
			break
		}
		err = m.putObject(ctx, buildFileName(view.PUBLISHED_SOURCES_ARCHIVES_TABLE, publishedSourceArchive.Checksum), publishedSourceArchive.Data)
		if err != nil {
			log.Infof("%d published_sources_archives were uploaded to minio storage, before error was received", offset)
			break
		}
		checksums = append(checksums, publishedSourceArchive.Checksum)
		offset++
	}
	return checksums, err
}

func (m minioStorageServiceImpl) UploadFile(ctx context.Context, tableName, entityId string, content []byte) error {
	start := time.Now()
	err := m.putObject(ctx, buildFileName(tableName, entityId), content)
	utils.PerfLog(time.Since(start).Milliseconds(), 500, "UploadFile: upload file to Minio")
	if err != nil {
		return err
	}
	return nil
}

func (m minioStorageServiceImpl) putObject(ctx context.Context, fileName string, content []byte) error {
	_, err := m.minioClient.client.PutObject(ctx, m.creds.BucketName, fileName, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (m minioStorageServiceImpl) GetFile(ctx context.Context, tableName, entityId string) ([]byte, error) {
	return m.getFile(ctx, buildFileName(tableName, entityId))
}

// fullFileName - tableName/entity_id.zip
func (m minioStorageServiceImpl) getFile(ctx context.Context, fullFileName string) ([]byte, error) {
	minioObject, err := m.minioClient.client.GetObject(ctx, m.creds.BucketName, fullFileName, minio.GetObjectOptions{})
	if err != nil {
		log.Warn(err)
		return nil, err
	}
	// GetObject owns a goroutine that is released only by Close
	defer minioObject.Close()
	minioObjectContent, err := io.ReadAll(minioObject)
	return minioObjectContent, err
}

func (m minioStorageServiceImpl) RemoveFiles(ctx context.Context, tableName string, entityIds []string) error {
	minioObjectsChan := make(chan minio.ObjectInfo, len(entityIds))
	utils.SafeAsync(func() {
		for _, id := range entityIds {
			minioObjectsChan <- minio.ObjectInfo{Key: buildFileName(tableName, id)}
		}
		defer close(minioObjectsChan)
	})
	errMsg := make([]string, 0)
	errChan := m.minioClient.client.RemoveObjects(ctx, m.creds.BucketName, minioObjectsChan, minio.RemoveObjectsOptions{})
	for removeError := range errChan {
		errMsg = append(errMsg, removeError.Err.Error())
	}
	if len(errMsg) > 0 {
		return errors.New(strings.Join(errMsg, ". "))
	}
	return nil
}

func bucketExists(ctx context.Context, minioClient *minio.Client, bucketName string) (bool, error) {
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func buildFileName(tableName, entityId string) string {
	return fmt.Sprintf("%s/%s.zip", tableName, entityId)
}

func getEntityId(folderName string, fileName string) string {
	if strings.Contains(fileName, folderName) && strings.Contains(fileName, ".zip") {
		entityIdDotZip := strings.ReplaceAll(fileName, folderName, "")
		return strings.ReplaceAll(entityIdDotZip, ".zip", "")
	}
	return ""
}

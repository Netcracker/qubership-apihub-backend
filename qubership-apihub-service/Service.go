package main

import (
	"context"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/responder"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security/idp/providers"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service/cleanup"
	"github.com/Netcracker/qubership-apihub-commons-go/api-spec-exposer/config"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/exception"
	mController "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/controller"
	mRepository "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/repository"
	mService "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/migration/service"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/cache"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/client"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/controller"
	midldleware "github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/middleware"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/repository"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/security"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/service"

	exposer "github.com/Netcracker/qubership-apihub-commons-go/api-spec-exposer"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

func init() {
	logFilePath := os.Getenv("LOG_FILE_PATH") //Example: /logs/apihub.log
	var mw io.Writer
	if logFilePath != "" {
		mw = io.MultiWriter(
			os.Stdout,
			&lumberjack.Logger{
				Filename: logFilePath,
				MaxSize:  10, // megabytes
			},
		)
	} else {
		mw = os.Stdout
	}
	log.SetFormatter(&prefixed.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
		ForceFormatting: true,
	})
	logLevel, err := log.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		logLevel = log.InfoLevel
	}
	log.SetLevel(logLevel)
	log.SetOutput(mw)
}

func main() {
	systemInfoService, err := service.NewSystemInfoService()
	if err != nil {
		panic(err)
	}

	if err := utils.ValidateTLSAtStartup(); err != nil {
		log.Fatalf("TLS configuration failed: %v", err)
	}
	basePath := systemInfoService.GetBasePath()

	// Create router and server to expose live and ready endpoints during initialization
	readyChan := make(chan bool)
	migrationPassedChan := make(chan bool)
	initSrvStoppedChan := make(chan bool)
	r := mux.NewRouter()
	// r.Use(midldleware.PrometheusMiddleware) todo figure out why breaks streaming
	r.Use(midldleware.WriteDeadlineMiddleware)
	r.SkipClean(true)
	r.UseEncodedPath()
	healthController := controller.NewHealthController(readyChan)
	r.HandleFunc("/live", healthController.HandleLiveRequest).Methods(http.MethodGet)
	r.HandleFunc("/ready", healthController.HandleReadyRequest).Methods(http.MethodGet)
	initSrv := makeServer(systemInfoService, r)

	creds := systemInfoService.GetCredsFromEnv()

	cp := db.NewConnectionProvider(creds)

	migrationRunRepository := mRepository.NewMigrationRunRepository(cp)
	buildCleanupRepository := repository.NewBuildCleanupRepository(cp)
	transitionRepository := repository.NewTransitionRepository(cp)
	buildResultRepository := repository.NewBuildResultRepository(cp)
	publishedRepository, err := repository.NewPublishedRepositoryPG(cp)
	if err != nil {
		log.Error("Failed to create PublishedRepository: " + err.Error())
		panic("Failed to create PublishedRepository: " + err.Error())
	}
	minioStorageCreds := systemInfoService.GetMinioStorageCreds()
	minioStorageService := service.NewMinioStorageService(buildResultRepository, publishedRepository, minioStorageCreds)
	dbMigrationService, err := mService.NewDBMigrationService(cp, migrationRunRepository, buildCleanupRepository, transitionRepository, systemInfoService, minioStorageService)
	if err != nil {
		log.Error("Failed create dbMigrationService: " + err.Error())
		panic("Failed create dbMigrationService: " + err.Error())
	}

	go func(initSrvStoppedChan chan bool) { // Do not use safe async here to enable panic
		log.Debugf("Starting init srv")
		_ = initSrv.ListenAndServe()
		log.Debugf("Init srv closed")
		initSrvStoppedChan <- true
		close(initSrvStoppedChan)
	}(initSrvStoppedChan)

	go func(migrationReadyChan chan bool) { // Do not use safe async here to enable panic
		passed := <-migrationPassedChan
		err := initSrv.Shutdown(context.Background())
		if err != nil {
			log.Fatalf("Failed to shutdown initial server")
		}
		if !passed {
			log.Fatalf("Stopping server since migration failed")
		}
		migrationReadyChan <- true
		close(migrationReadyChan)
		close(migrationPassedChan)
	}(readyChan)

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() { // Do not use safe async here to enable panic
		defer wg.Done()

		currentVersion, newVersion, migrationRequired, err := dbMigrationService.Migrate(basePath)
		if err != nil {
			log.Error("Failed perform DB migration: " + err.Error())
			time.Sleep(time.Second * 10) // Give a chance to read the unrecoverable error
			panic("Failed perform DB migration: " + err.Error())
		}
		// to perform migrations, which could not be implemented with "pure" SQL
		err = dbMigrationService.SoftMigrateDb(currentVersion, newVersion, migrationRequired)
		if err != nil {
			log.Errorf("Failed to perform db migrations: %v", err.Error())
			time.Sleep(time.Second * 10) // Give a chance to read the unrecoverable error
			panic("Failed to perform db migrations: " + err.Error())
		}

		migrationPassedChan <- true
	}()

	wg.Wait()
	_ = <-initSrvStoppedChan // wait for the init srv to stop to avoid multiple servers started race condition
	log.Infof("Migration step passed, continue initialization")

	favoritesRepository, err := repository.NewFavoritesRepositoryPG(cp)
	if err != nil {
		log.Error("Failed to create FavoriteRepository: " + err.Error())
		panic("Failed to create FavoriteRepository: " + err.Error())
	}

	usersRepository, err := repository.NewUserRepositoryPG(cp)
	if err != nil {
		log.Error("Failed to create UsersRepository: " + err.Error())
		panic("Failed to create UsersRepository: " + err.Error())
	}
	apihubApiKeyRepository, err := repository.NewApihubApiKeyRepositoryPG(cp)
	if err != nil {
		log.Error("Failed to create ApihubApiKeyRepository: " + err.Error())
		panic("Failed to create ApihubApiKeyRepository: " + err.Error())
	}
	buildRepository, err := repository.NewBuildRepositoryPG(cp)
	if err != nil {
		log.Error("Failed to create BuildRepository: " + err.Error())
		panic("Failed to create BuildRepository: " + err.Error())
	}

	roleRepository := repository.NewRoleRepository(cp)
	operationRepository := repository.NewOperationRepository(cp)
	ddlContractRepository := repository.NewDDLContractRepository(cp)
	mcpContractRepository := repository.NewMCPContractRepository(cp)
	businessMetricRepository := repository.NewBusinessMetricRepository(cp)

	activityTrackingRepository := repository.NewActivityTrackingRepository(cp)

	versionCleanupRepository := repository.NewVersionCleanupRepository(cp)
	comparisonCleanupRepository := repository.NewComparisonCleanupRepository(cp)

	personalAccessTokenRepository := repository.NewPersonalAccessTokenRepository(cp)

	packageExportConfigRepository := repository.NewPackageExportConfigRepository(cp)

	exportRepository := repository.NewExportRepository(cp)

	systemStatsRepository := repository.NewSystemStatsRepository(cp)

	deletedDataCleanupRepository := repository.NewSoftDeletedDataCleanupRepository(cp)

	unreferencedDataCleanupRepository := repository.NewUnreferencedDataCleanupRepository(cp)

	lockRepo := repository.NewLockRepository(cp)

	olricProvider, err := cache.NewOlricProvider(systemInfoService.GetOlricConfig())
	if err != nil {
		log.Error("Failed to create olricProvider: " + err.Error())
		panic("Failed to create olricProvider: " + err.Error())
	}

	privateUserPackageService := service.NewPrivateUserPackageService(publishedRepository, usersRepository, roleRepository, favoritesRepository)
	userService := service.NewUserService(usersRepository, systemInfoService, privateUserPackageService)

	lockService := service.NewLockService(lockRepo, systemInfoService.GetInstanceId())

	monitoringService := service.NewMonitoringService(cp)

	cleanupService := cleanup.NewCleanupService(cp)
	if err := cleanupService.CreateRevisionsCleanupJob(publishedRepository, migrationRunRepository, versionCleanupRepository, monitoringService, lockService, systemInfoService.GetInstanceId(), systemInfoService.GetRevisionsCleanupSchedule(), systemInfoService.GetRevisionsCleanupDeleteLastRevision(), systemInfoService.GetRevisionsCleanupDeleteReleaseRevisions(), systemInfoService.GetRevisionsTTLDays()); err != nil {
		log.Error("Failed to start revisions cleaning job" + err.Error())
	}
	if err := cleanupService.CreateComparisonsCleanupJob(publishedRepository, migrationRunRepository, comparisonCleanupRepository, lockService, systemInfoService.GetInstanceId(), systemInfoService.GetComparisonCleanupSchedule(), systemInfoService.GetComparisonCleanupTimeout(), systemInfoService.GetComparisonsTTLDays()); err != nil {
		log.Error("Failed to start comparisons cleaning job" + err.Error())
	}
	if err := cleanupService.CreateSoftDeletedDataCleanupJob(publishedRepository, migrationRunRepository, deletedDataCleanupRepository, lockService, systemInfoService.GetInstanceId(), systemInfoService.GetSoftDeletedDataCleanupSchedule(), systemInfoService.GetSoftDeletedDataCleanupTimeout(), systemInfoService.GetSoftDeletedDataTTLDays()); err != nil {
		log.Error("Failed to start soft deleted data cleaning job" + err.Error())
	}
	if err := cleanupService.CreateUnreferencedDataCleanupJob(migrationRunRepository, unreferencedDataCleanupRepository, lockService, systemInfoService.GetInstanceId(), systemInfoService.GetUnreferencedDataCleanupSchedule(), systemInfoService.GetUnreferencedDataCleanupTimeout()); err != nil {
		log.Error("Failed to start unreferenced data cleaning job" + err.Error())
	}
	if err := cleanupService.CreateMaintenanceVacuumCleanupJob(migrationRunRepository, lockService, systemInfoService.GetInstanceId(), systemInfoService.GetMaintenanceVacuumCleanupSchedule(), systemInfoService.GetMaintenanceVacuumCleanupTimeout()); err != nil {
		log.Error("Failed to start maintenance vacuum cleaning job" + err.Error())
	}

	packageVersionEnrichmentService := service.NewPackageVersionEnrichmentService(publishedRepository)
	activityTrackingService := service.NewActivityTrackingService(activityTrackingRepository, publishedRepository, userService)
	operationService := service.NewOperationService(operationRepository, publishedRepository, packageVersionEnrichmentService)
	roleService := service.NewRoleService(roleRepository, userService, activityTrackingService, publishedRepository)
	ptHandler := service.NewPackageTransitionHandler(transitionRepository)
	publishNotificationService := service.NewPublishNotificationService(olricProvider)
	publishedService := service.NewPublishedService(publishedRepository, buildRepository, favoritesRepository, operationRepository, ddlContractRepository, activityTrackingService, monitoringService, minioStorageService, systemInfoService, publishNotificationService, roleService)
	portalService := service.NewPortalService(basePath, publishedService, publishedRepository)

	operationGroupService := service.NewOperationGroupService(operationRepository, publishedRepository, exportRepository, packageVersionEnrichmentService, activityTrackingService, publishedService, systemInfoService)
	ddlContractServiceForVersion := service.NewDDLContractService(ddlContractRepository, publishedRepository, packageVersionEnrichmentService)
	mcpContractServiceForVersion := service.NewMCPContractService(mcpContractRepository, publishedRepository, packageVersionEnrichmentService)
	versionService := service.NewVersionService(favoritesRepository, publishedRepository, publishedService, operationRepository, exportRepository, operationService, activityTrackingService, systemInfoService, packageVersionEnrichmentService, portalService, versionCleanupRepository, operationGroupService, monitoringService, roleService, ddlContractServiceForVersion, mcpContractServiceForVersion)
	packageService := service.NewPackageService(favoritesRepository, publishedRepository, versionService, roleService, activityTrackingService, monitoringService, operationGroupService, usersRepository, ptHandler, systemInfoService)

	logsService := service.NewLogsService()
	apihubApiKeyService := service.NewApihubApiKeyService(apihubApiKeyRepository, publishedRepository, activityTrackingService, userService, roleRepository, roleService.IsSysadm, systemInfoService)

	refResolverService := service.NewRefResolverService(publishedRepository)
	buildProcessorService := service.NewBuildProcessorService(buildRepository, refResolverService)
	buildService := service.NewBuildService(buildRepository, buildProcessorService, publishedService, systemInfoService, packageService, refResolverService)

	packageExportConfigService := service.NewPackageExportConfigService(packageExportConfigRepository, packageService)

	exportService := service.NewExportService(exportRepository, buildService, packageExportConfigService)

	buildResultService := service.NewBuildResultService(buildResultRepository, buildRepository, publishedRepository, systemInfoService, minioStorageService, publishedService, exportService)
	versionService.SetBuildService(buildService)
	operationGroupService.SetBuildService(buildService)

	excelService := service.NewExcelService(publishedRepository, versionService, operationService, packageService, ddlContractServiceForVersion, mcpContractServiceForVersion)
	comparisonService := service.NewComparisonService(publishedRepository, operationRepository, packageVersionEnrichmentService, ddlContractServiceForVersion)
	businessMetricService := service.NewBusinessMetricService(businessMetricRepository)

	dbCleanupService := service.NewDBCleanupService(buildCleanupRepository, migrationRunRepository, minioStorageService, systemInfoService)
	if err := dbCleanupService.CreateCleanupJob(systemInfoService.GetBuildsCleanupSchedule()); err != nil {
		log.Error("Failed to start cleaning job" + err.Error())
	}

	transitionService := service.NewTransitionService(transitionRepository, publishedRepository)
	transformationService := service.NewTransformationService(publishedRepository, operationRepository, packageVersionEnrichmentService)

	zeroDayAdminService := service.NewZeroDayAdminService(userService, roleService, usersRepository, systemInfoService)

	personalAccessTokenService := service.NewPersonalAccessTokenService(personalAccessTokenRepository, userService, roleService)

	tokenRevocationService := service.NewTokenRevocationService(olricProvider, systemInfoService.GetRefreshTokenDurationSec())
	systemStatsService := service.NewSystemStatsService(systemStatsRepository)

	ddlContractService := ddlContractServiceForVersion
	mcpContractService := mcpContractServiceForVersion

	mcpService := service.NewMCPService(systemInfoService, operationService, packageService, versionService, monitoringService, roleService)

	responder := responder.NewResponder(systemInfoService.ShowDebugInResponse())
	authHandler, err := security.NewAuthHandler(userService, roleService, apihubApiKeyService, personalAccessTokenService, systemInfoService, tokenRevocationService, responder)

	if err != nil {
		log.Fatalf("Can't setup authHandler. Error - %s", err.Error())
	}
	ephemeralFileRepository := repository.NewEphemeralFileRepositoryPG(cp)
	ephemeralFileService := service.NewEphemeralFileService(systemInfoService, ephemeralFileRepository)
	ephemeralFileController := controller.NewEphemeralFileController(ephemeralFileService, responder, authHandler)
	ephemeralFileCleanup := service.NewEphemeralFileCleanupService(ephemeralFileRepository, lockService)
	if err := ephemeralFileCleanup.StartCleanupJob(systemInfoService.GetEphemeralFilesCleanupSchedule(), systemInfoService.GetEphemeralFileDirectory()); err != nil {
		log.Warnf("Failed to start ephemeral files cleanup: %v", err)
	}

	aiChatEnabled := isAiChatEnabled(systemInfoService)
	var aiChatController *controller.AiChatController
	if aiChatEnabled {
		log.Info("ai-chat: routes and cleanup jobs are ENABLED")
		aiChatRepository := repository.NewAiChatRepositoryPG(cp)
		llmClient, err := client.NewOpenAILlmClient(systemInfoService.GetAiChatConfig().OpenAI)
		if err != nil {
			log.Fatalf("Failed to create OpenAI LLM client: %v", err)
		}
		aiChatsService := service.NewAiChatsService(aiChatRepository)
		aiChatTurnService, err := service.NewAiChatTurnService(systemInfoService, aiChatRepository, llmClient, mcpService, ephemeralFileService, authHandler.MintEphemeralFileToken)
		if err != nil {
			log.Fatalf("Failed to create AiChatTurnService: %v", err)
		}
		aiChatController = controller.NewAiChatController(aiChatsService, aiChatTurnService, monitoringService, responder)
		aiChatCleanup := service.NewAiChatCleanupService(aiChatRepository, lockService)
		aiCfg := systemInfoService.GetAiChatConfig()
		if err := aiChatCleanup.StartChatRetentionJob(aiCfg.CleanupSchedule, aiCfg.RetentionDays, aiCfg.PinnedForeverCount); err != nil {
			log.Warnf("Failed to start ai chat retention cleanup: %v", err)
		}
	}

	idpManager, err := providers.NewIDPManager(systemInfoService.GetAuthConfig(), systemInfoService.GetAllowedHosts(), systemInfoService.IsProductionMode(), userService, responder, authHandler)
	if err != nil {
		log.Error("Failed to initialize external IDP: " + err.Error())
		panic("Failed to initialize external IDP: " + err.Error())
	}

	publishedController := controller.NewPublishedController(publishedService, portalService, roleService, responder)

	logsController := controller.NewLogsController(logsService, roleService, responder)
	systemInfoController := controller.NewSystemInfoController(systemInfoService, dbMigrationService, responder)
	sysAdminController := controller.NewSysAdminController(roleService, responder)
	apihubApiKeyController := controller.NewApihubApiKeyController(apihubApiKeyService, roleService, responder)
	cleanupController := controller.NewCleanupController(cleanupService, responder)

	playgroundProxyController, err := controller.NewPlaygroundProxyController(systemInfoService, responder)
	if err != nil {
		log.Fatalf("Failed to create PlaygroundProxyController: %v", err)
	}
	publishV2Controller := controller.NewPublishV2Controller(buildService, publishedService, buildResultService, roleService, systemInfoService, packageService, responder)
	exportController := controller.NewExportController(publishedService, portalService, roleService, excelService, versionService, monitoringService, exportService, packageService, responder)

	packageController := controller.NewPackageController(packageService, publishedService, portalService, roleService, monitoringService, ptHandler, responder)
	versionController := controller.NewVersionController(versionService, roleService, monitoringService, ptHandler, roleService.IsSysadm, excelService, systemInfoService.GetShareabilityReportSizeLimitMB(), responder)
	roleController := controller.NewRoleController(roleService, responder)
	samlAuthController := controller.NewSamlAuthController(userService, systemInfoService, idpManager, responder, authHandler) //deprecated
	authController := controller.NewAuthController(systemInfoService, idpManager, responder)
	userController := controller.NewUserController(userService, privateUserPackageService, roleService, responder)
	jwtPubKeyController := controller.NewJwtPubKeyController(responder, authHandler)
	logoutController := controller.NewLogoutController(tokenRevocationService, systemInfoService, responder)
	operationController := controller.NewOperationController(roleService, operationService, buildService, monitoringService, ptHandler, responder)
	operationGroupController := controller.NewOperationGroupController(roleService, operationGroupService, versionService, systemInfoService, packageService, responder)
	searchController := controller.NewSearchController(operationService, versionService, monitoringService, ddlContractService, mcpContractService, responder)
	dataMigrationController := mController.NewTempMigrationController(dbMigrationService, roleService.IsSysadm, responder)
	activityTrackingController := controller.NewActivityTrackingController(activityTrackingService, roleService, ptHandler, responder)
	comparisonController := controller.NewComparisonController(operationService, versionService, buildService, roleService, comparisonService, monitoringService, ptHandler, responder)
	transitionController := controller.NewTransitionController(transitionService, roleService.IsSysadm, responder)
	businessMetricController := controller.NewBusinessMetricController(businessMetricService, excelService, roleService.IsSysadm, responder)
	transformationController := controller.NewTransformationController(roleService, buildService, versionService, transformationService, operationGroupService, responder)
	minioStorageController := controller.NewMinioStorageController(minioStorageCreds, minioStorageService, roleService, responder)
	personalAccessTokenController := controller.NewPersonalAccessTokenController(personalAccessTokenService, responder)
	packageExportConfigController := controller.NewPackageExportConfigController(roleService, packageExportConfigService, ptHandler, responder)
	systemStatsController := controller.NewSystemStatsController(systemStatsService, roleService, responder)
	internalDocsController := controller.NewInternalDocumentController(publishedService, roleService, responder)
	ddlContractController := controller.NewDDLContractController(roleService, ddlContractService, ptHandler, responder)
	mcpContractController := controller.NewMCPContractController(roleService, mcpContractService, ptHandler, responder)

	mcpController := controller.NewMCPController(mcpService)
	buildController := controller.NewBuildController(buildResultService, buildService, roleService.IsSysadm, responder)
	adminPublishedController := controller.NewAdminPublishedController(publishedService, roleService.IsSysadm, systemInfoService.GetPublishArchiveSizeLimitMB(), responder)

	r.HandleFunc("/api/v1/system/info", authHandler.Secure(systemInfoController.GetSystemInfo)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/system/configuration", samlAuthController.GetSystemSSOInfo_deprecated).Methods(http.MethodGet) //deprecated
	r.HandleFunc("/api/v2/system/configuration", authHandler.NoSecure(authController.GetSystemConfigurationInfo)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/debug/logs", authHandler.SecureUser(logsController.StoreLogs)).Methods(http.MethodPut)
	r.HandleFunc("/api/v1/debug/logs/setLevel", authHandler.Secure(logsController.SetLogLevel)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/debug/logs/checkLevel", authHandler.Secure(logsController.CheckLogLevel)).Methods(http.MethodGet)

	//Search
	r.HandleFunc("/api/v3/search/{searchLevel}", authHandler.SecureUser(searchController.Search_deprecated)).Methods(http.MethodPost) //TODO: add API key strategy after authorization fix
	r.HandleFunc("/api/v4/search/{searchLevel}", authHandler.SecureUser(searchController.Search)).Methods(http.MethodPost)            //TODO: add API key strategy after authorization fix

	r.HandleFunc("/api/v2/builders/{builderId}/tasks", authHandler.Secure(publishV2Controller.GetFreeBuild)).Methods(http.MethodPost)

	r.HandleFunc("/api/v2/packages", authHandler.Secure(packageController.CreatePackage)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}", authHandler.Secure(packageController.UpdatePackage)).Methods(http.MethodPatch)
	r.HandleFunc("/api/v2/packages/{packageId}", authHandler.Secure(packageController.DeletePackage)).Methods(http.MethodDelete)
	r.HandleFunc("/api/v2/packages/{packageId}/favor", authHandler.Secure(packageController.FavorPackage)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}/disfavor", authHandler.Secure(packageController.DisfavorPackage)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}", authHandler.Secure(packageController.GetPackage)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/status", authHandler.Secure(packageController.GetPackageStatus)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages", authHandler.Secure(packageController.GetPackagesList)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/publish/availableStatuses", authHandler.Secure(packageController.GetAvailableVersionStatusesForPublish_deprecated)).Methods(http.MethodGet) // deprecated

	r.HandleFunc("/api/v4/packages/{packageId}/apiKeys", authHandler.Secure(apihubApiKeyController.GetApiKeys)).Methods(http.MethodGet)
	r.HandleFunc("/api/v4/packages/{packageId}/apiKeys", authHandler.Secure(apihubApiKeyController.CreateApiKey)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}/apiKeys/{id}", authHandler.Secure(apihubApiKeyController.RevokeApiKey)).Methods(http.MethodDelete)

	r.HandleFunc("/api/v2/packages/{packageId}/members", authHandler.Secure(roleController.GetPackageMembers)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/members", authHandler.Secure(roleController.AddPackageMembers)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}/members/{userId}", authHandler.Secure(roleController.UpdatePackageMembers)).Methods(http.MethodPatch)
	r.HandleFunc("/api/v2/packages/{packageId}/members/{userId}", authHandler.Secure(roleController.DeletePackageMember)).Methods(http.MethodDelete)

	r.HandleFunc("/api/v2/packages/{packageId}/recalculateGroups", authHandler.Secure(packageController.RecalculateOperationGroups)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}/calculateGroups", authHandler.Secure(packageController.CalculateOperationGroups)).Methods(http.MethodGet)

	//api for extensions
	r.HandleFunc("/api/v2/users/{userId}/availablePackagePromoteStatuses", authHandler.Secure(roleController.GetAvailableUserPackagePromoteStatuses)).Methods(http.MethodPost)

	r.HandleFunc("/api/v2/packages/{packageId}/publish/{publishId}/status", authHandler.Secure(publishV2Controller.GetPublishStatus)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/publish/statuses", authHandler.Secure(publishV2Controller.GetPublishStatuses)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}/publish", authHandler.Secure(publishV2Controller.Publish)).Methods(http.MethodPost)
	r.HandleFunc("/api/v3/packages/{packageId}/publish/{publishId}/status", authHandler.Secure(publishV2Controller.SetPublishStatus)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/packages/{packageId}/publish/withOperationsGroup", authHandler.Secure(versionController.PublishFromCSV_deprecated)).Methods(http.MethodPost) //deprecated
	r.HandleFunc("/api/v2/packages/{packageId}/publish/withOperationsGroup/{apiType}", authHandler.Secure(versionController.PublishFromCSV)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/packages/{packageId}/publish/{publishId}/withOperationsGroup/status", authHandler.Secure(versionController.GetCSVDashboardPublishStatus)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/publish/{publishId}/withOperationsGroup/report", authHandler.Secure(versionController.GetCSVDashboardPublishReport)).Methods(http.MethodGet)

	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}", authHandler.Secure(versionController.GetPackageVersionContent)).Methods(http.MethodGet)
	r.HandleFunc("/api/v3/packages/{packageId}/versions", authHandler.Secure(versionController.GetPackageVersionsList)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}", authHandler.Secure(versionController.DeleteVersion)).Methods(http.MethodDelete)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}", authHandler.Secure(versionController.PatchVersion)).Methods(http.MethodPatch)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/recursiveDelete", authHandler.Secure(versionController.DeleteVersionsRecursively)).Methods(http.MethodPost)

	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/files/{slug}/raw", authHandler.Secure(versionController.GetVersionedContentFileRaw)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/sharedFiles/{sharedFileId}", authHandler.NoSecure(versionController.GetSharedContentFile)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/changes", authHandler.Secure(versionController.GetVersionChanges_deprecated)).Methods(http.MethodGet)   // deprecated
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/problems", authHandler.Secure(versionController.GetVersionProblems_deprecated)).Methods(http.MethodGet) // deprecated
	r.HandleFunc("/api/v2/sharedFiles", authHandler.Secure(versionController.SharePublishedFile)).Methods(http.MethodPost)

	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/doc", authHandler.Secure(exportController.GenerateVersionDoc)).Methods(http.MethodGet)           // deprecated
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/files/{slug}/doc", authHandler.Secure(exportController.GenerateFileDoc)).Methods(http.MethodGet) // deprecated

	r.HandleFunc("/api/v2/auth/saml", authHandler.NoSecure(samlAuthController.StartSamlAuthentication_deprecated)).Methods(http.MethodGet)   // deprecated
	r.HandleFunc("/login/sso/saml", authHandler.RefreshToken(samlAuthController.StartSamlAuthentication_deprecated)).Methods(http.MethodGet) // deprecated
	r.HandleFunc("/saml/acs", authHandler.NoSecure(samlAuthController.AssertionConsumerHandler_deprecated)).Methods(http.MethodPost)
	r.HandleFunc("/saml/metadata", authHandler.NoSecure(samlAuthController.ServeMetadata_deprecated)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/login/sso/{idpId}", authHandler.RefreshToken(authController.StartAuthentication)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/saml/{idpId}/acs", authHandler.NoSecure(authController.SAMLAssertionConsumerHandler)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/saml/{idpId}/metadata", authHandler.NoSecure(authController.ServeMetadata)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/oidc/{idpId}/callback", authController.OIDCCallbackHandler).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/logout", authHandler.SecureJWT(logoutController.Logout)).Methods(http.MethodPost)

	// Required for agent to verify apihub tokens
	r.HandleFunc("/api/v2/auth/publicKey", authHandler.NoSecure(jwtPubKeyController.GetRsaPublicKey)).Methods(http.MethodGet)
	// Required to verify api key for external authorization
	r.HandleFunc("/api/v2/auth/apiKey", authHandler.NoSecure(apihubApiKeyController.GetApiKeyByKey)).Methods(http.MethodGet)
	// Required to verify PAT for external authorization
	r.HandleFunc("/api/v2/auth/pat", authHandler.NoSecure(personalAccessTokenController.GetPatByPat)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/auth/apiKey/{apiKeyId}", authHandler.Secure(apihubApiKeyController.GetApiKeyById)).Methods(http.MethodGet)
	// Required for extensions to check Apihub auth. Just return 200 OK if authentication is passed.
	r.HandleFunc("/api/v1/auth/token", authHandler.SecureJWT(func(writer http.ResponseWriter, request *http.Request) {})).Methods(http.MethodGet)

	r.HandleFunc("/api/v2/users/{userId}/profile/avatar", authHandler.NoSecure(userController.GetUserAvatar)).Methods(http.MethodGet) // Should not be secured! FE renders avatar as <img src='avatarUrl' and it couldn't include auth header
	r.HandleFunc("/api/v2/users", authHandler.Secure(userController.GetUsers)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/users/{userId}", authHandler.Secure(userController.GetUserById)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/users/{userId}/space", authHandler.Secure(userController.CreatePrivatePackageForUser)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/space", authHandler.SecureUser(userController.CreatePrivateUserPackage)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/space", authHandler.SecureUser(userController.GetPrivateUserPackage)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/user", authHandler.SecureUser(userController.GetExtendedUser_deprecated)).Methods(http.MethodGet) //deprecated
	r.HandleFunc("/api/v2/user", authHandler.SecureUser(userController.GetExtendedUser)).Methods(http.MethodGet)

	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/changes/summary", authHandler.Secure(comparisonController.GetComparisonChangesSummary)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/operations", authHandler.Secure(operationController.GetOperationList)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/operations/{operationId}", authHandler.Secure(operationController.GetOperation)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/operations/{operationId}/changes", authHandler.Secure(operationController.GetOperationChanges)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/operations/{operationId}/models/{modelName}/usages", authHandler.Secure(operationController.GetOperationModelUsages)).Methods(http.MethodGet)
	r.HandleFunc("/api/v4/packages/{packageId}/versions/{version}/{apiType}/changes", authHandler.Secure(operationController.GetOperationsChanges)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/tags", authHandler.Secure(operationController.GetOperationsTags)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/deprecated", authHandler.Secure(operationController.GetDeprecatedOperationsList)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/operations/{operationId}/deprecatedItems", authHandler.Secure(operationController.GetOperationDeprecatedItems)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/deprecated/summary", authHandler.Secure(operationController.GetDeprecatedOperationsSummary)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/operations/{operationId}/changes/summary", authHandler.Secure(operationController.GetOperationChangesSummary)).Methods(http.MethodGet)

	// DDL Contract routes.
	// Static sub-routes (changes, export/*) are registered before the {ddlEntityId} wildcard
	// so gorilla/mux does not shadow them.
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/ddl/entities", authHandler.Secure(ddlContractController.ListDdlEntities)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/ddl/changes", authHandler.Secure(ddlContractController.GetChangedDdlEntities)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/ddl/export/entities", authHandler.Secure(exportController.GenerateDdlEntitiesExcelReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/ddl/export/changes", authHandler.Secure(exportController.GenerateDdlChangesExcelReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/ddl/entities/{ddlEntityId}", authHandler.Secure(ddlContractController.GetDdlEntity)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/ddl/entities/{ddlEntityId}/changes", authHandler.Secure(ddlContractController.GetDdlEntityChanges)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/ddl/entities/{ddlEntityId}/changes/summary", authHandler.Secure(ddlContractController.GetDdlEntityChangesSummary)).Methods(http.MethodGet)

	// MCP Contract routes ({entity} ∈ {inits, tools, prompts, resources}).
	// mcp/export/{entity} is registered before mcp/{entity}/{mcpEntityId} so it is not shadowed.
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/mcp/export/{entity}", authHandler.Secure(exportController.GenerateMcpEntitiesExcelReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/mcp/{entity}", authHandler.Secure(mcpContractController.ListMcpEntities)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/mcp/{entity}/{mcpEntityId}", authHandler.Secure(mcpContractController.GetMcpEntity)).Methods(http.MethodGet)

	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/documents/{slug}", authHandler.Secure(versionController.GetVersionedDocument)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/documents", authHandler.Secure(versionController.GetVersionDocuments)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/documents/{slug}/shareability", authHandler.Secure(versionController.UpdateDocumentShareability)).Methods(http.MethodPatch)
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/references", authHandler.Secure(versionController.GetVersionReferencesV3)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/sources", authHandler.Secure(publishedController.GetVersionSources)).Methods(http.MethodGet)
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/revisions", authHandler.Secure(versionController.GetVersionRevisionsList)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/sourceData", authHandler.Secure(publishedController.GetPublishedVersionSourceDataConfig)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/config", authHandler.Secure(publishedController.GetPublishedVersionBuildConfig)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/copy", authHandler.Secure(versionController.CopyVersion)).Methods(http.MethodPost)

	r.HandleFunc("/api/v4/packages/{packageId}/activity", authHandler.Secure(activityTrackingController.GetActivityHistoryForPackage)).Methods(http.MethodGet)
	r.HandleFunc("/api/v4/activity", authHandler.SecureUser(activityTrackingController.GetActivityHistory)).Methods(http.MethodGet) //TODO: add API key strategy after authorization fix

	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/groups", authHandler.Secure(operationGroupController.CreateOperationGroup)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/groups/{groupName}", authHandler.Secure(operationGroupController.DeleteOperationGroup)).Methods(http.MethodDelete)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/groups/{groupName}", authHandler.Secure(operationGroupController.GetGroupedOperations)).Methods(http.MethodGet)
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/groups/{groupName}", authHandler.Secure(operationGroupController.UpdateOperationGroup)).Methods(http.MethodPatch)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/{apiType}/groups/{groupName}/template", authHandler.Secure(operationGroupController.GetGroupExportTemplate)).Methods(http.MethodGet)

	r.HandleFunc("/playground/proxy", authHandler.SecureProxy(playgroundProxyController.Proxy))

	r.HandleFunc("/api/v2/admins", authHandler.Secure(sysAdminController.GetSystemAdministrators)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/admins", authHandler.Secure(sysAdminController.AddSystemAdministrator)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/admins/{userId}", authHandler.Secure(sysAdminController.DeleteSystemAdministrator)).Methods(http.MethodDelete)
	r.HandleFunc("/api/v2/permissions", authHandler.Secure(roleController.GetExistingPermissions)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/roles", authHandler.Secure(roleController.CreateRole)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/roles", authHandler.Secure(roleController.GetExistingRoles)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/roles/{roleId}", authHandler.Secure(roleController.UpdateRole)).Methods(http.MethodPatch)
	r.HandleFunc("/api/v2/roles/{roleId}", authHandler.Secure(roleController.DeleteRole)).Methods(http.MethodDelete)
	r.HandleFunc("/api/v2/roles/changeOrder", authHandler.Secure(roleController.SetRoleOrder)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/packages/{packageId}/availableRoles", authHandler.Secure(roleController.GetAvailablePackageRoles)).Methods(http.MethodGet)

	r.HandleFunc("/api/internal/migrate/operations", authHandler.Secure(dataMigrationController.StartOpsMigration)).Methods(http.MethodPost)
	r.HandleFunc("/api/internal/migrate/operations/{migrationId}", authHandler.Secure(dataMigrationController.GetMigrationReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/internal/migrate/operations/{migrationId}/suspiciousBuilds", authHandler.Secure(dataMigrationController.GetSuspiciousBuilds)).Methods(http.MethodGet)
	r.HandleFunc("/api/internal/migrate/operations/{migrationId}/perf", authHandler.Secure(dataMigrationController.GetMigrationPerfReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/internal/migrate/operations/cancel", authHandler.Secure(dataMigrationController.CancelRunningMigrations)).Methods(http.MethodPost)

	r.HandleFunc("/api/v2/admin/transition/move", authHandler.Secure(transitionController.MoveOrRenamePackage)).Methods(http.MethodPost)
	r.HandleFunc("/api/v2/admin/transition/move/{id}", authHandler.Secure(transitionController.GetMoveStatus)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/admin/transition/activity", authHandler.Secure(transitionController.ListActivities)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/admin/transition", authHandler.Secure(transitionController.ListPackageTransitions)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/builds", authHandler.Secure(buildController.ListBuilds)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/builds/{buildId}", authHandler.Secure(buildController.GetBuild)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/admin/builds/{buildId}/result", authHandler.Secure(buildController.GetBuildResult)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/admin/builds/{buildId}/sources", authHandler.Secure(buildController.GetBuildSources)).Methods(http.MethodGet)

	r.HandleFunc("/api/v2/admin/packages/{packageId}/versions/{version}/sources", authHandler.Secure(adminPublishedController.ReplaceVersionSources)).Methods(http.MethodPut)

	r.HandleFunc("/api/v2/admin/system/stats", authHandler.Secure(systemStatsController.GetSystemStats)).Methods(http.MethodGet)

	r.HandleFunc("/api/v2/compare", authHandler.Secure(comparisonController.CompareTwoVersions)).Methods(http.MethodPost)

	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/changes/export", authHandler.Secure(exportController.GenerateApiChangesExcelReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/export/changes", authHandler.Secure(exportController.GenerateApiChangesExcelReportV3)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/export/operations", authHandler.Secure(exportController.GenerateOperationsExcelReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/{apiType}/export/operations/deprecated", authHandler.Secure(exportController.GenerateDeprecatedOperationsExcelReport)).Methods(http.MethodGet)
	r.HandleFunc("/api/v2/packages/{packageId}/versions/{version}/export/shareability-report", authHandler.Secure(exportController.GenerateShareabilityReport)).Methods(http.MethodGet)

	r.Path("/metrics").Handler(promhttp.Handler())
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/build/groups/{groupName}/buildType/{buildType}", authHandler.Secure(transformationController.TransformDocuments_deprecated_2)).Methods(http.MethodPost)             //deprecated
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/export/groups/{groupName}/buildType/{buildType}", authHandler.Secure(exportController.ExportOperationGroupAsOpenAPIDocuments_deprecated_2)).Methods(http.MethodGet) //deprecated
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/groups/{groupName}/documents", authHandler.Secure(transformationController.GetDataForDocumentsTransformation)).Methods(http.MethodGet)

	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/groups/{groupName}/publish", authHandler.Secure(operationGroupController.StartOperationGroupPublish)).Methods(http.MethodPost)
	r.HandleFunc("/api/v3/packages/{packageId}/versions/{version}/{apiType}/groups/{groupName}/publish/{publishId}/status", authHandler.Secure(operationGroupController.GetOperationGroupPublishStatus)).Methods(http.MethodGet)

	r.HandleFunc("/api/v2/businessMetrics", authHandler.Secure(businessMetricController.GetBusinessMetrics)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/publishHistory", authHandler.Secure(versionController.GetPublishedVersionsHistory)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/personalAccessToken", authHandler.Secure(personalAccessTokenController.CreatePAT)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/personalAccessToken", authHandler.Secure(personalAccessTokenController.ListPATs)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/personalAccessToken/{id}", authHandler.Secure(personalAccessTokenController.DeletePAT)).Methods(http.MethodDelete)

	r.HandleFunc("/api/v1/packages/{packageId}/exportConfig", authHandler.Secure(packageExportConfigController.GetConfig)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/exportConfig", authHandler.Secure(packageExportConfigController.SetConfig)).Methods(http.MethodPatch)

	r.HandleFunc("/api/v1/export", authHandler.Secure(exportController.StartAsyncExport)).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/export/{exportId}/status", authHandler.Secure(exportController.GetAsyncExportStatus)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/deleted/packages", authHandler.Secure(packageController.GetDeletedPackagesList)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/deleted/packages/{packageId}/versions", authHandler.Secure(versionController.GetDeletedPackageVersionsList)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/deleted/packages/{packageId}/versions/{version}", authHandler.Secure(versionController.GetDeletedPackageVersionContent)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/version-internal-documents", authHandler.Secure(internalDocsController.GetVersionInternalDocuments)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/version-internal-documents/{hash}", authHandler.Secure(internalDocsController.GetVersionInternalDocumentData)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/packages/{packageId}/versions/{version}/comparison-internal-documents", authHandler.Secure(internalDocsController.GetComparisonInternalDocuments)).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/comparison-internal-documents/{hash}", authHandler.Secure(internalDocsController.GetComparisonInternalDocumentData)).Methods(http.MethodGet)

	r.HandleFunc("/api/v1/shareability/bulk-update", authHandler.Secure(versionController.BulkUpdateDocumentShareability)).Methods(http.MethodPost)

	//debug + cleanup
	if !systemInfoService.GetSystemInfo().ProductionMode {
		r.HandleFunc("/api/internal/users/{userId}/systemRole", authHandler.Secure(roleController.TestSetUserSystemRole)).Methods(http.MethodPost)
		r.HandleFunc("/api/internal/users", authHandler.NoSecure(userController.CreateInternalUser)).Methods(http.MethodPost)
		r.HandleFunc("/api/v2/auth/local", authHandler.NoSecure(authHandler.CreateLocalUserToken_deprecated)).Methods(http.MethodPost) //deprecated
		r.HandleFunc("/api/v3/auth/local", authHandler.NoSecure(authHandler.CreateLocalUserToken)).Methods(http.MethodPost)
		r.HandleFunc("/api/v3/auth/local/refresh", authHandler.RefreshToken(responder.RedirectHandler(systemInfoService.GetAPIHubUrl()))).Methods(http.MethodGet)

		r.HandleFunc("/api/internal/clear/{testId}", authHandler.Secure(cleanupController.ClearTestData)).Methods(http.MethodDelete)

		r.PathPrefix("/debug/").Handler(http.DefaultServeMux)

		r.HandleFunc("/api/internal/minio/download", authHandler.Secure(minioStorageController.DownloadFilesFromMinioToDatabase)).Methods(http.MethodPost)
	}

	r.HandleFunc("/api/v1/ephemeral-files/{fileId}", authHandler.NoSecure(ephemeralFileController.Download)).Methods(http.MethodGet)

	if aiChatEnabled {
		r.HandleFunc("/api/v1/ai-chat/chats", authHandler.Secure(aiChatController.ListChats)).Methods(http.MethodGet)
		r.HandleFunc("/api/v1/ai-chat/chats", authHandler.Secure(aiChatController.CreateChat)).Methods(http.MethodPost)
		r.HandleFunc("/api/v1/ai-chat/chats/{chatId}", authHandler.Secure(aiChatController.GetChat)).Methods(http.MethodGet)
		r.HandleFunc("/api/v1/ai-chat/chats/{chatId}", authHandler.Secure(aiChatController.UpdateChat)).Methods(http.MethodPatch)
		r.HandleFunc("/api/v1/ai-chat/chats/{chatId}", authHandler.Secure(aiChatController.DeleteChat)).Methods(http.MethodDelete)
		r.HandleFunc("/api/v1/ai-chat/chats/{chatId}/messages", authHandler.Secure(aiChatController.ListMessages)).Methods(http.MethodGet)
		r.HandleFunc("/api/v1/ai-chat/chats/{chatId}/messages", authHandler.Secure(aiChatController.SendMessage)).Methods(http.MethodPost)
		r.HandleFunc("/api/v1/ai-chat/chats/{chatId}/messages/stream", authHandler.Secure(aiChatController.SendMessageStream)).Methods(http.MethodPost)
	}

	mcpHandler := mcpController.MakeMCPServer()
	r.Handle("/api/v1/mcp/", authHandler.SecureMCP(mcpHandler))

	discoveryConfig := config.DiscoveryConfig{
		ScanDirectory: systemInfoService.GetApiSpecDirectory(),
	}
	specExposer := exposer.New(discoveryConfig)
	discoveryResult := specExposer.Discover()
	if len(discoveryResult.Errors) > 0 {
		for _, err := range discoveryResult.Errors {
			log.Errorf("Error during API specifications discovery: %v", err)
		}
		panic("Failed to expose API specifications")
	}
	if len(discoveryResult.Warnings) > 0 {
		for _, warning := range discoveryResult.Warnings {
			log.Warnf("Warning during API specifications discovery: %s", warning)
		}
	}
	for _, endpointConfig := range discoveryResult.Endpoints {
		log.Debugf("Registering API specification endpoint with path: %s and spec metadata: %+v", endpointConfig.Path, endpointConfig.SpecMetadata)
		r.HandleFunc(endpointConfig.Path, endpointConfig.Handler).Methods(http.MethodGet)
	}

	portalFs := http.FileServer(http.Dir(basePath + "/static/portal"))

	knownPathPrefixes := []string{
		"/api/",
		"/v3/",
		"/login/",
		"/playground/",
		"/saml/",
		"/ws/",
		"/metrics",
	}
	for _, prefix := range knownPathPrefixes {
		//add routing for unknown paths with known path prefixes
		r.PathPrefix(prefix).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			xForwardedFor, remoteAddr := utils.RequestorIPFields(r)
			log.WithFields(log.Fields{
				"method":          r.Method,
				"uri":             r.RequestURI,
				"x_forwarded_for": xForwardedFor,
				"remote_addr":     remoteAddr,
			}).Warn("Requested unknown endpoint")

			responder.RespondWithCustomError(w, &exception.CustomError{
				Status:  http.StatusMisdirectedRequest,
				Message: "Requested unknown endpoint",
			})
		})
	}

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: return not implemented if request matches /api /ws
		w.Header().Add("Cache-Control", "max-age=57600") // 16h
		if r.URL.Path != "/" {
			fullPath := basePath + "/static/portal/" + strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			_, err := os.Stat(fullPath)
			if err != nil { // Redirect unknown requests to frontend
				r.URL.Path = "/"
			}
			portalFs.ServeHTTP(w, r)
		} else {
			portalFs.ServeHTTP(w, r) // portal is default app
		}
	})

	debug.SetGCPercent(30)

	srv := makeServer(systemInfoService, r)

	utils.SafeAsync(func() {
		if err := zeroDayAdminService.CreateZeroDayAdmin(); err != nil {
			log.Errorf("Failed to create zero day admin user: %s", err)
		}

		if err := apihubApiKeyService.CreateSystemApiKey(); err != nil {
			log.Errorf("Failed to create system api key: %s", err)
		}
	})

	if systemInfoService.MonitoringEnabled() {
		utils.SafeAsync(func() {
			metrics.RegisterAllPrometheusApplicationMetrics()
		})
	}

	if systemInfoService.IsMinioStorageActive() {
		utils.SafeAsync(func() {
			err := minioStorageService.UploadFilesToBucket()
			if err != nil {
				log.Errorf("MINIO error - %s", err.Error())
			}
		})
	}

	utils.SafeAsync(func() {
		exportService.StartCleanupOldResultsJob()
	})

	dbMigrationService.StartOpsMigrationRestoreProc(context.Background())

	log.Fatalf("Http server returned error: %v", srv.ListenAndServe())
}

func isAiChatEnabled(sis service.SystemInfoService) bool {
	return sis.GetAiChatConfig().Enabled
}

func makeServer(systemInfoService service.SystemInfoService, r *mux.Router) *http.Server {
	listenAddr := systemInfoService.GetListenAddress()

	log.Infof("Listen addr = %s", listenAddr)

	var corsOptions []handlers.CORSOption

	corsOptions = append(corsOptions, handlers.AllowedHeaders([]string{"Connection", "Accept-Encoding", "Content-Encoding", "X-Requested-With", "Content-Type", "Authorization"}))

	allowedOrigins := systemInfoService.GetAllowedOrigins()
	if len(allowedOrigins) > 0 {
		corsOptions = append(corsOptions, handlers.AllowedOrigins(allowedOrigins))
	}
	corsOptions = append(corsOptions, handlers.AllowedMethods([]string{"GET", "HEAD", "POST", "PUT", "OPTIONS"}))

	// ReadTimeout limits the time for the client to send the full request (headers + body).
	// The timer starts when the connection is accepted and applies to the entire read phase:
	//   - During header reading: if headers aren't fully received within the deadline, the
	//     server closes the connection immediately and the handler is never called.
	//   - During body reading (inside handler): the remaining time from the same deadline
	//     applies to r.Body reads. If the deadline expires, r.Body.Read() returns a timeout
	//     error — the connection is NOT dropped automatically, the handler must handle the error.
	//   - For requests with no body (e.g., GET), the body phase is irrelevant.
	// This protects against slow or abandoned connections consuming server resources.
	//
	// WriteTimeout is intentionally NOT set. Go's WriteTimeout starts its timer when request
	// headers are read and covers the entire handler execution plus response writing.
	// This makes it unsuitable for long-running requests: a handler that legitimately processes
	// for 4 minutes would have only 1 minute left for writing (with WriteTimeout=300s).
	// The connection won't be dropped at the timeout mark — it stays open while the handler
	// runs — but the write will immediately fail when the handler finally tries to respond.
	// Instead, we use:
	//   - http.ResponseController.SetWriteDeadline per-request (see middleware/WriteDeadlineMiddleware.go) to set
	//     a deadline only on the response writing phase, independent of processing time.
	//   - Context with deadline for processing time control (planned, not yet implemented).
	corsHandler := handlers.CORS(corsOptions...)(r)
	compressedHandler := handlers.CompressHandler(corsHandler)
	handler := midldleware.NewSelectiveCompressionHandler(corsHandler, compressedHandler)

	return &http.Server{
		Handler:     handler,
		Addr:        listenAddr,
		ReadTimeout: 60 * time.Second,
	}
}

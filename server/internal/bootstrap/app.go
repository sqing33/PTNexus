package bootstrap

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/config"
	"github.com/pt-nexus/server/internal/http/handler"
	migratehandler "github.com/pt-nexus/server/internal/http/handler/migrate"
	"github.com/pt-nexus/server/internal/http/middleware"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/platform/netproxy"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service"
	migrationflow "github.com/pt-nexus/server/internal/service/migrationflow"
)

// App 封装 Gin 引擎实例，供主程序启动 HTTP 服务。
type App struct {
	Engine        *gin.Engine
	trackerWorker *service.TrackerService
}

// NewApp 初始化配置、数据库、服务与路由，并返回可运行的应用实例。
func NewApp() (*App, error) {
	paths := config.ResolveRuntimePaths()
	cfgManager, err := config.NewManager(paths)
	if err != nil {
		return nil, fmt.Errorf("初始化配置管理器失败: %w", err)
	}
	if err := netproxy.Install(cfgManager.NetworkProxyConfig()); err != nil {
		return nil, fmt.Errorf("初始化网络代理失败: %w", err)
	}

	store, err := repository.NewStore(paths)
	if err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}
	schemaManager := repository.NewSchemaManager(store)
	if err := schemaManager.EnsureSchema(); err != nil {
		return nil, fmt.Errorf("初始化数据库表结构失败: %w", err)
	}
	if err := schemaManager.SyncSitesFromJSON(paths.SitesData); err != nil {
		logx.Warnf("启动", "同步站点配置失败 path=%s err=%v", paths.SitesData, err)
	}

	authService := service.NewAuthService(cfgManager)
	settingsService := service.NewSettingsService(cfgManager)
	statsRepo := repository.NewStatsRepository(store)
	statsService := service.NewStatsService(statsRepo, cfgManager)
	trackerService := service.NewTrackerService(statsRepo, cfgManager)
	trackerService.Start()
	torrentDataRepo := repository.NewTorrentDataRepository(store)
	torrentDataService := service.NewTorrentDataService(torrentDataRepo, cfgManager)
	torrentDataService.SetIYUULogger(func(level string, message string) {
		settingsService.AppendIYUULog(level, message)
	})
	localQueryService := service.NewLocalQueryService(repository.NewLocalQueryRepository(store), cfgManager, paths.DataDir)
	crossSeedService := service.NewCrossSeedService(repository.NewCrossSeedRepository(store))

	siteRepo := repository.NewSiteRepository(store)
	torrentRepo := repository.NewTorrentRepository(store)
	migrateRepo := repository.NewMigrateRepository(store)
	queueRepo := repository.NewPublishQueueRepository(store)
	publishLogRepo := repository.NewPublishLogRepository(store)

	migrateService := migrationflow.NewMigrateService(migrateRepo, cfgManager)
	migrateService.InitPublishQueue(queueRepo, statsRepo)
	migrateService.InitPublishLogs(publishLogRepo)
	migrateService.StartPublishQueueWorker()
	torrentTransferService := service.NewTorrentTransferService(migrateRepo, cfgManager)

	settingsService.SetIYUUTrigger(func() map[string]any {
		settings := cfgManager.Get()
		iyuuSettings, _ := settings["iyuu_settings"].(map[string]any)
		pathsRaw, _ := iyuuSettings["selected_paths"].([]any)
		paths := make([]string, 0, len(pathsRaw))
		for _, raw := range pathsRaw {
			if value, ok := raw.(string); ok && strings.TrimSpace(value) != "" {
				paths = append(paths, strings.TrimSpace(value))
			}
		}
		if len(paths) == 0 {
			return map[string]any{"success": false, "message": "IYUU 未配置任何路径，无法触发查询"}
		}

		results := make([]map[string]any, 0, len(paths))
		totalProcessed := 0
		successCount := 0
		for _, path := range paths {
			result, status := torrentDataService.QueryIYUU(map[string]any{"save_path": path, "limit": 200})
			entry := map[string]any{"path": path, "status": status, "result": result}
			results = append(results, entry)

			okSuccess := false
			if successValue, ok := result["success"].(bool); ok {
				okSuccess = successValue
			}
			if status == 200 && okSuccess {
				successCount++
				if stats, ok := result["stats"].(map[string]any); ok {
					switch typed := stats["processed_groups"].(type) {
					case int:
						totalProcessed += typed
					case int64:
						totalProcessed += int(typed)
					case float64:
						totalProcessed += int(typed)
					}
				}
			}
		}

		success := successCount > 0
		message := fmt.Sprintf("IYUU 查询完成：成功 %d/%d 个路径", successCount, len(paths))
		return map[string]any{
			"success": success,
			"message": message,
			"data": map[string]any{
				"paths":            paths,
				"processed_groups": totalProcessed,
				"results":          results,
			},
		}
	})

	authHandler := handler.NewAuthHandler(authService)
	settingsHandler := handler.NewSettingsHandler(settingsService, torrentRepo, torrentDataRepo, siteRepo)
	configHandler := handler.NewConfigHandler(settingsService)
	sitesHandler := handler.NewSitesHandler(siteRepo)
	torrentsHandler := handler.NewTorrentsHandler(torrentRepo)
	torrentDataHandler := handler.NewTorrentDataHandler(torrentDataService)
	statsHandler := handler.NewStatsHandler(statsService)
	localQueryHandler := handler.NewLocalQueryHandler(localQueryService)
	crossSeedHandler := handler.NewCrossSeedHandler(crossSeedService)
	migrateHandler := migratehandler.New(migrateService)
	torrentTransferHandler := handler.NewTorrentTransferHandler(torrentTransferService)
	logsHandler := handler.NewLogsHandler(service.NewLogExportService())

	gin.SetMode(resolveGinMode())
	engine := gin.New()
	gin.DefaultWriter = logx.Writer()
	gin.DefaultErrorWriter = logx.Writer()
	if err := engine.SetTrustedProxies(buildTrustedProxies()); err != nil {
		return nil, fmt.Errorf("设置可信代理失败: %w", err)
	}
	engine.Use(
		middleware.RequestContextMiddleware(),
		gin.Logger(),
		gin.Recovery(),
		cors.New(cors.Config{
			AllowAllOrigins:  true,
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders:     []string{"Content-Type", "Authorization", "X-Requested-With", "X-Internal-API-Key", middleware.RequestIDHeader},
			AllowCredentials: false,
		}),
	)
	engine.Use(middleware.JWTGuard(authService))

	registerRoutes(
		engine,
		paths.StaticDir,
		authHandler,
		settingsHandler,
		configHandler,
		sitesHandler,
		torrentsHandler,
		torrentDataHandler,
		statsHandler,
		localQueryHandler,
		crossSeedHandler,
		migrateHandler,
		torrentTransferHandler,
		logsHandler,
	)

	return &App{Engine: engine, trackerWorker: trackerService}, nil
}

// registerRoutes 统一注册所有 API 路由与静态资源回退逻辑。
func registerRoutes(
	engine *gin.Engine,
	staticDir string,
	authHandler *handler.AuthHandler,
	settingsHandler *handler.SettingsHandler,
	configHandler *handler.ConfigHandler,
	sitesHandler *handler.SitesHandler,
	torrentsHandler *handler.TorrentsHandler,
	torrentDataHandler *handler.TorrentDataHandler,
	statsHandler *handler.StatsHandler,
	localQueryHandler *handler.LocalQueryHandler,
	crossSeedHandler *handler.CrossSeedHandler,
	migrateHandler *migratehandler.Handler,
	torrentTransferHandler *handler.TorrentTransferHandler,
	logsHandler *handler.LogsHandler,
) {
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "服务正常", "service": "pt-nexus-go"})
	})

	authAPI := engine.Group("/api/auth")
	{
		authAPI.POST("/login", authHandler.Login)
		authAPI.GET("/status", authHandler.Status)
		authAPI.POST("/change_password", authHandler.ChangePassword)
	}

	api := engine.Group("/api")
	{
		api.GET("/sites_list", sitesHandler.SitesList)
		api.GET("/sites", sitesHandler.Sites)
		api.POST("/sites/update", sitesHandler.UpdateSite)
		api.POST("/sites/delete", sitesHandler.DeleteSite)
		api.POST("/sites/update_cookie", sitesHandler.UpdateSiteCookie)
		api.GET("/sites/cookie_sync_targets", sitesHandler.CookieSyncTargets)
		api.POST("/sites/cookie_sync_batch", sitesHandler.BatchUpdateSiteCookies)
		api.POST("/cookiecloud/sync", settingsHandler.CookieCloudSync)
		api.POST("/test_connection", settingsHandler.TestConnection)
		api.POST("/upload_image", settingsHandler.UploadImage)
		api.GET("/settings", settingsHandler.GetSettings)
		api.POST("/settings", settingsHandler.UpdateSettings)
		api.GET("/downloader_info", settingsHandler.DownloaderInfo)
		api.GET("/downloaders_list", settingsHandler.DownloadersList)
		api.GET("/all_downloaders", settingsHandler.AllDownloaders)
		api.GET("/ui_settings", settingsHandler.GetUISettings)
		api.POST("/ui_settings", settingsHandler.SaveUISettings)
		api.GET("/ui_settings/cross_seed", settingsHandler.GetCrossSeedUISettings)
		api.POST("/ui_settings/cross_seed", settingsHandler.SaveCrossSeedUISettings)
		api.GET("/ui_settings/publish_logs", settingsHandler.GetPublishLogsUISettings)
		api.POST("/ui_settings/publish_logs", settingsHandler.SavePublishLogsUISettings)
		api.GET("/upload_settings", settingsHandler.GetUploadSettings)
		api.POST("/upload_settings", settingsHandler.SaveUploadSettings)
		api.GET("/iyuu/settings", settingsHandler.GetIYUUSettings)
		api.POST("/iyuu/settings", settingsHandler.SaveIYUUSettings)
		api.POST("/iyuu/trigger_query", settingsHandler.TriggerIYUUQuery)
		api.GET("/iyuu/logs", settingsHandler.IYUUlogs)
		api.GET("/migration/check", settingsHandler.CheckMigrationNeeded)
		api.POST("/migration/execute", settingsHandler.ExecuteMigration)
		api.GET("/migration/history", settingsHandler.MigrationHistory)
		api.GET("/paths", torrentsHandler.Paths)
		api.GET("/settings/cross_seed", settingsHandler.GetCrossSeedSettings)
		api.POST("/settings/cross_seed", settingsHandler.SaveCrossSeedSettings)
		api.GET("/settings/cross_seed/publish_concurrency_info", settingsHandler.PublishConcurrencyInfo)

		api.GET("/data", torrentDataHandler.Data)
		api.POST("/refresh_data", torrentDataHandler.RefreshData)
		api.GET("/cached_sites", torrentDataHandler.CachedSites)
		api.POST("/iyuu_query", torrentDataHandler.IYUUQuery)
		api.POST("/iyuu_query_batch", torrentDataHandler.IYUUQueryBatch)
		api.GET("/iyuu_query_batch_progress", torrentDataHandler.IYUUQueryBatchProgress)

		api.GET("/chart_data", statsHandler.ChartData)
		api.GET("/speed_data", statsHandler.SpeedData)
		api.GET("/recent_speed_data", statsHandler.RecentSpeedData)
		api.GET("/speed_chart_data", statsHandler.SpeedChartData)
		api.GET("/site_stats", statsHandler.SiteStats)
		api.GET("/group_stats", statsHandler.GroupStats)

		api.GET("/cross-seed-data/unique-paths", crossSeedHandler.UniquePaths)
		api.GET("/cross-seed-data", crossSeedHandler.Data)
		api.POST("/cross-seed-data/test-no-auth", crossSeedHandler.TestNoAuth)
		api.GET("/cross-seed-data/test-no-auth", crossSeedHandler.TestNoAuth)
		api.POST("/cross-seed-data/delete", crossSeedHandler.DeleteData)
		api.DELETE("/cross-seed-data/delete", crossSeedHandler.DeleteData)

		api.POST("/utils/parse_title", migrateHandler.ParseTitle)
		api.POST("/media/validate", migrateHandler.MediaValidate)
		api.POST("/migrate_torrent", migrateHandler.MigrateTorrent)

		api.GET("/publish_logs", migrateHandler.PublishLogs)
		api.DELETE("/publish_logs/:id", migrateHandler.DeletePublishLog)
	}

	migrateAPI := engine.Group("/api/migrate")
	{
		migrateAPI.GET("/get_db_seed_info", migrateHandler.GetDBSeedInfo)
		migrateAPI.POST("/download_torrent_only", migrateHandler.DownloadTorrentOnly)
		migrateAPI.POST("/fetch_and_store", migrateHandler.FetchAndStore)
		migrateAPI.POST("/update_db_seed_info", migrateHandler.UpdateDBSeedInfo)
		migrateAPI.POST("/update_screenshot_review_status", migrateHandler.UpdateScreenshotReviewStatus)
		migrateAPI.POST("/publish", migrateHandler.Publish)
		migrateAPI.POST("/publish_batch/start", migrateHandler.PublishBatchStart)
		migrateAPI.GET("/publish_batch/status/:batch_id", migrateHandler.PublishBatchStatus)
		migrateAPI.POST("/publish_batch/cancel/:batch_id", migrateHandler.PublishBatchCancel)
		migrateAPI.GET("/publish_batch/stream/:batch_id", migrateHandler.PublishBatchStream)
		migrateAPI.POST("/publish_queue/enqueue", migrateHandler.PublishQueueEnqueue)
		migrateAPI.POST("/publish_queue/enqueue_batch", migrateHandler.PublishQueueEnqueueBatch)
		migrateAPI.DELETE("/publish_queue/tasks/:queue_task_id", migrateHandler.PublishQueueDeleteTask)

		migrateAPI.POST("/batch_fetch_seed_data", migrateHandler.BatchFetchSeedData)
		migrateAPI.POST("/update_preview_data", migrateHandler.UpdatePreviewData)
		migrateAPI.POST("/get_aggregated_torrents", migrateHandler.GetAggregatedTorrents)
		migrateAPI.GET("/batch_fetch_progress", migrateHandler.BatchFetchProgress)
		migrateAPI.GET("/logs/stream/:task_id", migrateHandler.LogsStream)
		migrateAPI.GET("/task_monitor", migrateHandler.TaskMonitor)

		migrateAPI.POST("/get_downloader_info", migrateHandler.GetDownloaderInfo)
		migrateAPI.POST("/add_to_downloader", migrateHandler.AddToDownloader)
		migrateAPI.POST("/refresh_mediainfo_async", migrateHandler.RefreshMediainfoAsync)

		migrateAPI.GET("/bdinfo_records", migrateHandler.BDInfoRecords)
		migrateAPI.GET("/bdinfo_tasks", migrateHandler.BDInfoTasks)
		migrateAPI.GET("/bdinfo_status/:seed_id", migrateHandler.BDInfoStatus)
		migrateAPI.POST("/refresh_bdinfo/:seed_id", migrateHandler.RefreshBDInfo)
		migrateAPI.POST("/cleanup_bdinfo_process", migrateHandler.CleanupBDInfoProcess)
		migrateAPI.POST("/restart_bdinfo", migrateHandler.RestartBDInfo)
		migrateAPI.GET("/bdinfo_sse/:seed_id", migrateHandler.BDInfoSSE)
		migrateAPI.POST("/bdinfo/progress", migrateHandler.BDInfoProgress)
		migrateAPI.POST("/bdinfo/complete", migrateHandler.BDInfoComplete)
	}

	torrentTransferAPI := engine.Group("/api/torrent/transfer")
	{
		torrentTransferAPI.POST("/prepare", torrentTransferHandler.Prepare)
		torrentTransferAPI.POST("/similar", torrentTransferHandler.Similar)
		torrentTransferAPI.POST("/execute", torrentTransferHandler.Execute)
		torrentTransferAPI.POST("/pause", torrentTransferHandler.Pause)
		torrentTransferAPI.POST("/export", torrentTransferHandler.Export)
		torrentTransferAPI.POST("/add", torrentTransferHandler.Add)
	}

	localQueryAPI := engine.Group("/api/local_query")
	{
		localQueryAPI.GET("/scan/cache", localQueryHandler.ScanCache)
		localQueryAPI.GET("/paths", localQueryHandler.Paths)
		localQueryAPI.GET("/downloaders_with_paths", localQueryHandler.DownloadersWithPaths)
		localQueryAPI.POST("/scan", localQueryHandler.Scan)
		localQueryAPI.GET("/analyze_duplicates", localQueryHandler.AnalyzeDuplicates)
	}

	sitesAPI := engine.Group("/api/sites")
	{
		sitesAPI.POST("/set_not_exist", sitesHandler.SetNotExist)
		sitesAPI.POST("/update_comment", sitesHandler.UpdateComment)
		sitesAPI.GET("/status", sitesHandler.SitesStatus)
	}

	configAPI := engine.Group("/api/config")
	{
		configAPI.GET("/source_priority", configHandler.GetSourcePriority)
		configAPI.POST("/source_priority", configHandler.SaveSourcePriority)
		configAPI.GET("/batch_fetch_filters", configHandler.GetBatchFetchFilters)
		configAPI.POST("/batch_fetch_filters", configHandler.SaveBatchFetchFilters)
		configAPI.GET("/cross_seed_review_filter", configHandler.GetCrossSeedReviewFilter)
		configAPI.POST("/cross_seed_review_filter", configHandler.SaveCrossSeedReviewFilter)
		configAPI.GET("/tags", configHandler.GetTags)
		configAPI.POST("/tags", configHandler.SaveTags)
	}

	logsAPI := engine.Group("/api/logs")
	{
		logsAPI.GET("/export", logsHandler.Export)
	}

	engine.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "接口不存在"})
			return
		}

		requestPath := c.Request.URL.Path
		cleaned := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
		if strings.HasPrefix(cleaned, "/..") {
			c.String(http.StatusBadRequest, "非法路径")
			return
		}
		if cleaned != "/" && !strings.HasSuffix(cleaned, "/") {
			rel := strings.TrimPrefix(cleaned, "/")
			candidate := filepath.Join(staticDir, rel)
			if stat, err := os.Stat(candidate); err == nil && stat.Mode().IsRegular() {
				c.File(candidate)
				return
			}
		}

		indexPath := filepath.Join(staticDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			c.File(indexPath)
			return
		}
		c.String(http.StatusNotFound, "PT Nexus Go 接口服务")
	})
}

// resolveGinMode 读取 GIN_MODE，默认使用 release 降低无关调试输出。
func resolveGinMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GIN_MODE")))
	switch mode {
	case gin.DebugMode, gin.TestMode:
		return mode
	default:
		return gin.ReleaseMode
	}
}

// buildTrustedProxies 汇总可信代理配置，避免信任所有代理带来的安全风险。
func buildTrustedProxies() []string {
	trusted := []string{"127.0.0.1", "::1"}
	extra := strings.Split(os.Getenv("TRUSTED_PROXIES"), ",")
	for _, proxy := range extra {
		trimmed := strings.TrimSpace(proxy)
		if trimmed == "" {
			continue
		}
		if net.ParseIP(trimmed) == nil && !strings.Contains(trimmed, "/") {
			continue
		}
		already := false
		for _, existing := range trusted {
			if existing == trimmed {
				already = true
				break
			}
		}
		if !already {
			trusted = append(trusted, trimmed)
		}
	}
	return trusted
}

// buildAllowedOrigins 汇总允许跨域来源，支持默认值与环境变量扩展。
func buildAllowedOrigins() []string {
	allowed := []string{
		"http://localhost:35275",
		"http://127.0.0.1:5173",
		"http://localhost:5173",
		"http://127.0.0.1:5174",
		"http://localhost:5174",
		"http://127.0.0.1:5274",
		"http://localhost:5274",
		"http://127.0.0.1:5275",
		"http://localhost:5275",
	}
	extra := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
	for _, origin := range extra {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		already := false
		for _, existing := range allowed {
			if existing == trimmed {
				already = true
				break
			}
		}
		if !already {
			allowed = append(allowed, trimmed)
		}
	}
	return allowed
}

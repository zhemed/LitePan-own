package app

import (
	"context"

	"litepan/internal/account"
	"litepan/internal/cacheretention"
	"litepan/internal/config"
	"litepan/internal/domain"
	"litepan/internal/favorites"
	"litepan/internal/file"
	"litepan/internal/fusemount"
	"litepan/internal/fusereadcache"
	"litepan/internal/logx"
	"litepan/internal/offlinedownload"
	"litepan/internal/playback"
	"litepan/internal/settings"
	"litepan/internal/upload"
)

type servicesBundle struct {
	files            *file.Service
	uploads          *upload.Manager
	offlineDownloads *offlinedownload.Service
	playback         *playback.Service
	account          *account.Service
	fuse             *fusemount.Service
	fuseReadCache    *fusereadcache.Service
	cacheRetention   *cacheretention.Service
	favorites        *favorites.Service
}

func wireServices(ctx context.Context, cfg config.Config, logs *logx.Manager, st *storeBundle, core *coreBundle) *servicesBundle {
	favoritesSvc := favorites.NewService(cfg.DBPath, logs.For(logx.ModuleSystem))
	fileSvc := file.NewService(core.exec, core.cache, st.store.Accounts, core.bus, st.settings, core.listHits)
	fileSvc.SetLogger(logs.For(logx.ModuleFileOp))
	playbackSvc := playback.NewService(core.exec, core.cache)
	retentionSvc, retentionCoord := wireCacheRetention(st, fileSvc, core.cache, core.bus, logs)
	fuseReadCache := wireFuseReadCacheOrNil(ctx, cfg, logs, st, core.bus)
	offlineDownloadSvc := offlinedownload.New(offlinedownload.Options{
		Exec:     core.exec,
		Accounts: st.store.Accounts,
		Repo:     st.store.OfflineDownloads,
		Folders:  fileSvc,
		Settings: st.settings,
		DataDir:  cfg.DataDir,
		Bus:      core.bus,
		Log:      logs.For(logx.ModuleFileOp),
	})
	fuseSvc := fusemount.New(fusemount.Options{
		Repo:      st.store.FuseMounts,
		Configs:   st.store.Configs,
		Accounts:  st.store.Accounts,
		Notify:    st.store.Notifications,
		Files:     fileSvc,
		Playback:  playbackSvc,
		ReadCache: fuseReadCache,
		Bus:       core.bus,
		Log:       logs.For(logx.ModuleSystem),
	})
	_ = fuseSvc.PrepareMountRoot()
	lifecycle := &accountLifecycle{
		fuse:      fuseSvc,
		readCache: fuseReadCache,
		retention: retentionCoord,
		favorites: favoritesSvc,
		offline:   offlineDownloadSvc,
	}
	accountSvc := account.NewService(account.Options{
		Accounts:      st.store.Accounts,
		AuthStates:    st.store.AuthStates,
		Drivers:       core.drivers,
		Auth:          core.auth,
		Playback:      playbackSvc,
		MetadataCache: core.cache,
		Lifecycle:     lifecycle,
		OAuthURL: func(context.Context) string {
			return domain.NormalizeOAuthServerURL(st.settings.String(settings.KeyOAuthServerURL))
		},
	})
	uploadSvc := upload.NewManager(upload.Options{
		Exec:     core.exec,
		Files:    fileSvc,
		Playback: playbackSvc,
		Accounts: accountSvc,
		Repo:     st.store.UploadTasks,
		Settings: st.settings,
		Bus:      core.bus,
		DataDir:  cfg.DataDir,
		Log:      logs.For(logx.ModuleFileOp),
	})
	lifecycle.uploads = uploadSvc
	offlineDownloadSvc.SetUploads(uploadSvc)
	fuseSvc.SetUploads(uploadSvc)
	return &servicesBundle{
		files:            fileSvc,
		uploads:          uploadSvc,
		offlineDownloads: offlineDownloadSvc,
		playback:         playbackSvc,
		account:          accountSvc,
		fuse:             fuseSvc,
		fuseReadCache:    fuseReadCache,
		cacheRetention:   retentionSvc,
		favorites:        favoritesSvc,
	}
}

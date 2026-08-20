package upload

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/eventbus"
	"litepan/internal/file"
	"litepan/internal/playback"
	"litepan/internal/settings"
	"litepan/pkg/timeutil"
)

type AccountLookup interface {
	LookupUploadAccount(ctx context.Context, accountID int64) (name, driverType string, err error)
}

type Options struct {
	// ProtectedPaths 强制保护路径（本地源），清理逻辑绝不删除其下文件。
	ProtectedPaths []string
	Exec     *driverexec.Executor
	Files    *file.Service
	Playback *playback.Service
	Accounts AccountLookup
	Repo     domain.UploadTaskRepository
	Settings *settings.Service
	Bus      *eventbus.Bus
	DataDir  string
	Log      *slog.Logger
}

type Manager struct {
	exec     *driverexec.Executor
	files    *file.Service
	playback *playback.Service
	accounts AccountLookup
	repo     domain.UploadTaskRepository
	settings *settings.Service
	bus      *eventbus.Bus
	dataDir  string
	// protectedPaths 强制保护路径（本地源映射——飞牛备份源）。
	// removeLocalFile 拒绝删除这些路径下的任何文件。
	protectedPaths []string
	log            *slog.Logger

	mu                     sync.Mutex
	tasks                  map[string]*taskState
	queueOrder             int
	limit                  int
	runningUploads         int
	runningDownloads       int
	runCond                sync.Cond
	subs                   map[chan []byte]struct{}
	subMu                  sync.Mutex
	tempRegistry           *TempRegistry
	completedOfflineGroups map[string]struct{}
	runCtx                 context.Context
	runCancel              context.CancelFunc
	stopping               bool

	resumePersistMu sync.Mutex
	resumePersist   map[string]*time.Timer
}

func NewManager(opts Options) *Manager {
	runCtx, runCancel := context.WithCancel(context.Background())
	m := &Manager{
		exec:                   opts.Exec,
		files:                  opts.Files,
		playback:               opts.Playback,
		accounts:               opts.Accounts,
		repo:                   opts.Repo,
		settings:               opts.Settings,
		bus:                    opts.Bus,
		dataDir:                opts.DataDir,
		log:                    opts.Log,
		tasks:                  make(map[string]*taskState),
		limit:                  defaultLimit,
		subs:                   make(map[chan []byte]struct{}),
		completedOfflineGroups: make(map[string]struct{}),
		runCtx:                 runCtx,
		runCancel:              runCancel,
	}
	m.runCond.L = &m.mu
	if m.log == nil {
		m.log = slog.Default()
	}
	m.protectedPaths = protectedPathsFromOptions(opts)
	m.tempRegistry = NewTempRegistry()
	_ = m.RefreshConcurrencyLimit(context.Background())
	m.restoreTasks()
	m.initTempCleanup()
	return m
}

func (m *Manager) TempDir() string {
	return TempDir(m.dataDir)
}

func (m *Manager) Create(ctx context.Context, p CreateParams) (*Task, error) {
	tasks, err := m.createBatch(ctx, []CreateParams{p})
	if err != nil {
		return nil, err
	}
	return tasks[0], nil
}

// RenameTask 在上传任务尚未开始传输时更新目标文件名与目标目录。
// 返回 renamed=false 表示任务已开始或已完成，本次改名未生效（调用方不应因此报错）。
func (m *Manager) RenameTask(_ context.Context, taskID, newName, newTargetPath, newDisplayPath string) (bool, error) {
	name := strings.TrimSpace(newName)
	if name == "" {
		return false, nil
	}
	renamed := false
	m.patch(taskID, func(st *taskState) {
		if st.Status != StatusPending && st.Status != StatusPaused {
			return
		}
		st.FileName = name
		if newTargetPath != "" {
			st.TargetPath = newTargetPath
		}
		if newDisplayPath != "" {
			st.TargetDisplayPath = newDisplayPath
		}
		renamed = true
	})
	return renamed, nil
}

func (m *Manager) createBatch(ctx context.Context, params []CreateParams) ([]*Task, error) {
	if len(params) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "上传任务不能为空")
	}
	prepared := make([]CreateParams, len(params))
	for i, p := range params {
		var err error
		prepared[i], err = m.normalizeCreateParams(ctx, p)
		if err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return nil, domain.Errorf(domain.CodeInternal, "上传服务正在停止")
	}
	result := make([]*Task, len(prepared))
	created := make([]*taskState, 0, len(prepared))
	for i, p := range prepared {
		if existing := m.findByClientTaskIDLocked(p.ClientTaskID); existing != nil {
			result[i] = m.snapshot(existing)
			continue
		}
		st := m.newTaskStateLocked(p)
		m.tasks[st.TaskID] = st
		created = append(created, st)
		result[i] = m.snapshot(st)
	}
	m.mu.Unlock()

	persisted := make([]string, 0, len(created))
	for _, st := range created {
		if err := m.persistTask(st); err != nil {
			m.mu.Lock()
			for _, item := range created {
				delete(m.tasks, item.TaskID)
			}
			m.mu.Unlock()
			for _, id := range persisted {
				m.deletePersisted(id)
			}
			return nil, domain.Wrap(domain.CodeInternal, err)
		}
		persisted = append(persisted, st.TaskID)
	}
	if len(created) > 0 {
		m.broadcast()
	}
	for _, st := range created {
		go m.runTask(st.TaskID)
	}
	return result, nil
}

// Stop 取消并等待全部上传与跨盘任务退出，确保后续可以安全关闭任务仓储。
func (m *Manager) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if !m.stopping {
		m.stopping = true
		for _, st := range m.tasks {
			if st.Status == StatusRunning {
				st.cancelMode = "pause"
			}
		}
		m.runCancel()
	}
	done := make([]chan struct{}, 0, len(m.tasks))
	seen := make(map[chan struct{}]struct{}, len(m.tasks))
	for _, st := range m.tasks {
		if st.runDone == nil {
			continue
		}
		if _, ok := seen[st.runDone]; ok {
			continue
		}
		seen[st.runDone] = struct{}{}
		done = append(done, st.runDone)
	}
	m.mu.Unlock()
	m.runCond.Broadcast()

	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (m *Manager) normalizeCreateParams(ctx context.Context, p CreateParams) (CreateParams, error) {
	if p.TotalBytes < 0 {
		return CreateParams{}, domain.Errorf(domain.CodeValidation, "上传文件大小非法")
	}
	if p.AccountName == "" || p.DriverType == "" {
		if m.accounts == nil {
			return CreateParams{}, domain.Errorf(domain.CodeInternal, "上传服务未配置账号查询")
		}
		var err error
		p.AccountName, p.DriverType, err = m.accounts.LookupUploadAccount(ctx, p.AccountID)
		if err != nil {
			return CreateParams{}, err
		}
	}
	return p, nil
}

func (m *Manager) newTaskStateLocked(p CreateParams) *taskState {
	name := p.DisplayName
	if name == "" {
		name = p.FileName
	}
	sourceType := p.SourceType
	if sourceType == "" {
		sourceType = SourceTypeManual
	}
	phase := p.Phase
	if phase == "" {
		if sourceType == SourceTypeCrossTransfer {
			phase = PhaseDownloading
		} else {
			phase = PhaseUploading
		}
	}
	now := time.Now()
	m.queueOrder++
	order := m.queueOrder
	id := newTaskID()
	localPath := p.LocalPath
	if localPath == "" && sourceType == SourceTypeCrossTransfer {
		localPath = filepath.Join(m.TempDir(), id+filepath.Ext(name))
	}
	cleanupLocalMode := p.CleanupLocalMode
	cleanupLocalPath := p.CleanupLocalPath
	if cleanupLocalMode == "" && localPath != "" {
		switch sourceType {
		case SourceTypeManual, SourceTypeCrossTransfer:
			cleanupLocalMode = CleanupLocalFileOnSuccess
		}
	}
	if cleanupLocalPath == "" {
		cleanupLocalPath = localPath
	}
	message := "等待上传"
	switch sourceType {
	case SourceTypeCrossTransfer:
		message = "等待源盘下载"
	case SourceTypeOfflineHandoff:
		message = "等待离线文件上传"
	}
	st := &taskState{
		Task: Task{
			TaskID:            id,
			ClientTaskID:      p.ClientTaskID,
			AccountID:         p.AccountID,
			AccountName:       p.AccountName,
			DriverType:        p.DriverType,
			FileName:          name,
			SourceType:        sourceType,
			SourceAccountID:   p.SourceAccountID,
			SourceAccountName: p.SourceAccountName,
			SourceDriverType:  p.SourceDriverType,
			SourceFileID:      p.SourceFileID,
			RelPath:           p.RelPath,
			RelDir:            p.RelDir,
			TargetPath:        p.TargetPath,
			TargetDisplayPath: p.TargetDisplayPath,
			Status:            StatusPending,
			Phase:             phase,
			Message:           message,
			CleanupLocalMode:  cleanupLocalMode,
			CleanupLocalPath:  cleanupLocalPath,
			TotalBytes:        p.TotalBytes,
			QueueOrder:        order,
			CreatedAt:         timeutil.UnixFloat(now),
			UpdatedAt:         timeutil.UnixFloat(now),
		},
		localPath:      localPath,
		conflictPolicy: p.ConflictPolicy,
		runDone:        make(chan struct{}),
	}
	return st
}

func (m *Manager) CreateServerLocalTask(ctx context.Context, p ServerLocalCreateParams) (*Task, error) {
	tasks, err := m.CreateServerLocalTasks(ctx, []ServerLocalCreateParams{p})
	if err != nil {
		return nil, err
	}
	return tasks[0], nil
}

// CreateServerLocalTasks 会先校验全部本地文件，再一次性持久化并启动任务。
// 任一任务写库失败时，整批任务都不会进入运行队列。
func (m *Manager) CreateServerLocalTasks(ctx context.Context, params []ServerLocalCreateParams) ([]*Task, error) {
	if len(params) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "离线交棒任务不能为空")
	}
	result := make([]*Task, len(params))
	prepared := make([]CreateParams, 0, len(params))
	preparedIndexes := make([]int, 0, len(params))
	for i, p := range params {
		if strings.TrimSpace(p.LocalPath) == "" {
			return nil, domain.Errorf(domain.CodeValidation, "离线交棒缺少本地文件路径")
		}
		if p.ClientTaskID != "" {
			if existing := m.FindByClientTaskID(p.ClientTaskID); existing != nil {
				result[i] = existing
				continue
			}
		}
		info, err := os.Stat(p.LocalPath)
		if err != nil {
			return nil, domain.Wrap(domain.CodeNotFound, err)
		}
		if info.IsDir() {
			return nil, domain.Errorf(domain.CodeValidation, "离线交棒暂不支持目录，请提供文件路径")
		}
		size := p.TotalBytes
		if size <= 0 {
			size = info.Size()
		}
		prepared = append(prepared, CreateParams{
			ClientTaskID:      p.ClientTaskID,
			AccountID:         p.AccountID,
			AccountName:       p.AccountName,
			DriverType:        p.DriverType,
			FileName:          p.FileName,
			DisplayName:       p.DisplayName,
			SourceType:        SourceTypeOfflineHandoff,
			TargetPath:        p.TargetPath,
			TargetDisplayPath: p.TargetDisplayPath,
			LocalPath:         p.LocalPath,
			CleanupLocalMode:  p.CleanupLocalMode,
			CleanupLocalPath:  p.CleanupLocalPath,
			TotalBytes:        size,
			ConflictPolicy:    p.ConflictPolicy,
			Phase:             PhaseUploading,
		})
		preparedIndexes = append(preparedIndexes, i)
	}
	if len(prepared) == 0 {
		return result, nil
	}
	created, err := m.createBatch(ctx, prepared)
	if err != nil {
		return nil, err
	}
	for i, task := range created {
		result[preparedIndexes[i]] = task
	}
	return result, nil
}

func (m *Manager) findByClientTaskIDLocked(clientTaskID string) *taskState {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if clientTaskID == "" {
		return nil
	}
	for _, st := range m.tasks {
		if st.ClientTaskID == clientTaskID {
			return st
		}
	}
	return nil
}

func (m *Manager) FindByClientTaskID(clientTaskID string) *Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.findByClientTaskIDLocked(clientTaskID)
	if st == nil {
		return nil
	}
	return m.snapshot(st)
}

const offlineHandoffClientPrefix = "offline-handoff:"

func OfflineHandoffClientID(groupID string, index int) string {
	return fmt.Sprintf("%s%s:%d", offlineHandoffClientPrefix, strings.TrimSpace(groupID), index)
}

func offlineHandoffGroupID(clientTaskID string) (string, bool) {
	value := strings.TrimPrefix(strings.TrimSpace(clientTaskID), offlineHandoffClientPrefix)
	idx := strings.LastIndexByte(value, ':')
	if idx <= 0 || idx == len(value)-1 {
		return "", false
	}
	return value[:idx], true
}

func (m *Manager) List(_ context.Context, accountID int64) []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, 0, len(m.tasks))
	for _, st := range m.tasks {
		if accountID > 0 && st.AccountID != accountID {
			continue
		}
		out = append(out, *m.snapshot(st))
	}
	sortTasksDesc(out)
	return out
}

func (m *Manager) Get(_ context.Context, taskID string) (*Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.tasks[taskID]
	if !ok {
		return nil, false
	}
	t := m.snapshot(st)
	return t, true
}

// RemoveTasksByAccount 清理无法在账号删除后继续执行的上传任务。
// 目标账号被删除时全部清理；源账号被删除时，只清理仍处于源盘下载阶段的跨盘任务。
func (m *Manager) RemoveTasksByAccount(ctx context.Context, accountID int64) (int64, error) {
	if accountID <= 0 {
		return 0, nil
	}
	m.mu.Lock()
	ids := make([]string, 0)
	for id, st := range m.tasks {
		usesTarget := st.AccountID == accountID
		usesSource := st.SourceType == SourceTypeCrossTransfer &&
			st.SourceAccountID == accountID && st.Phase == PhaseDownloading
		if usesTarget || usesSource {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()

	var removed int64
	for _, id := range ids {
		found, err := m.Delete(ctx, id, false)
		if err != nil {
			return removed, err
		}
		if found {
			removed++
		}
	}
	return removed, nil
}

func (m *Manager) publishOfflineHandoffCompleted(taskID string) {
	if m.bus == nil {
		return
	}
	m.mu.Lock()
	current, ok := m.tasks[taskID]
	if !ok || current.SourceType != SourceTypeOfflineHandoff {
		m.mu.Unlock()
		return
	}
	groupID, grouped := offlineHandoffGroupID(current.ClientTaskID)
	groupKey := "task:" + current.TaskID
	eventTaskID := current.TaskID
	eventTask := current
	if grouped {
		groupKey = "group:" + groupID
		eventTaskID = groupID
		matched := 0
		for _, candidate := range m.tasks {
			candidateGroup, ok := offlineHandoffGroupID(candidate.ClientTaskID)
			if !ok || candidateGroup != groupID {
				continue
			}
			matched++
			if candidate.Status != StatusSuccess && candidate.Status != StatusSkipped {
				m.mu.Unlock()
				return
			}
			if displayPathDepth(candidate.TargetDisplayPath) < displayPathDepth(eventTask.TargetDisplayPath) {
				eventTask = candidate
			}
		}
		if matched == 0 {
			m.mu.Unlock()
			return
		}
	}
	if _, published := m.completedOfflineGroups[groupKey]; published {
		m.mu.Unlock()
		return
	}
	m.completedOfflineGroups[groupKey] = struct{}{}
	fileID, _ := current.Result["file_id"].(string)
	fileName, _ := current.Result["file_name"].(string)
	if fileName == "" {
		fileName = current.FileName
	}
	event := eventbus.OfflineDownloadCompleted{
		TaskID:            eventTaskID,
		AccountID:         current.AccountID,
		TargetParentID:    eventTask.TargetPath,
		TargetDisplayPath: eventTask.TargetDisplayPath,
		FileID:            fileID,
		FileName:          fileName,
	}
	m.mu.Unlock()
	m.bus.Publish(context.Background(), event)
}

func displayPathDepth(value string) int {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return 0
	}
	return strings.Count(value, "/") + 1
}

package upload

import (
	"context"
	"time"

	"litepan/pkg/speedsmoother"
)

const (
	progressInterval = 250 * time.Millisecond
	defaultLimit     = 3
)

// Task 是对外暴露的上传任务快照。
type Task struct {
	TaskID              string         `json:"task_id"`
	ClientTaskID        string         `json:"client_task_id,omitempty"`
	AccountID           int64          `json:"account_id"`
	AccountName         string         `json:"account_name"`
	DriverType          string         `json:"driver_type"`
	FileName            string         `json:"file_name"`
	SourceType          string         `json:"source_type,omitempty"`
	SourceAccountID     int64          `json:"source_account_id,omitempty"`
	SourceAccountName   string         `json:"source_account_name,omitempty"`
	SourceDriverType    string         `json:"source_driver_type,omitempty"`
	SourceFileID        string         `json:"source_file_id,omitempty"`
	RelPath             string         `json:"rel_path,omitempty"`
	RelDir              string         `json:"rel_dir,omitempty"`
	TargetPath          string         `json:"target_path"`
	TargetDisplayPath   string         `json:"target_display_path,omitempty"`
	Status              string         `json:"status"`
	Phase               string         `json:"phase,omitempty"`
	Progress            int            `json:"progress"`
	DownloadedBytes     int64          `json:"downloaded_bytes"`
	UploadedBytes       int64          `json:"uploaded_bytes"`
	SpeedBytesPerSecond float64        `json:"speed_bytes_per_second"`
	TotalBytes          int64          `json:"total_bytes"`
	Message             string         `json:"message"`
	Error               string         `json:"error,omitempty"`
	Result              map[string]any `json:"result,omitempty"`
	CleanupLocalMode    string         `json:"cleanup_local_mode,omitempty"`
	CleanupLocalPath    string         `json:"cleanup_local_path,omitempty"`
	QueueOrder          int            `json:"queue_order"`
	CreatedAt           float64        `json:"created_at"`
	UpdatedAt           float64        `json:"updated_at"`
}

const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusPaused   = "paused"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
	StatusSkipped  = "skipped"
)

const (
	SourceTypeManual         = "manual"
	SourceTypeCrossTransfer  = "cross_transfer"
	SourceTypeOfflineHandoff = "offline_handoff"
)

const (
	PhaseDownloading = "downloading"
	PhaseUploading   = "uploading"
)

const (
	CleanupLocalNone          = ""
	CleanupLocalFileOnSuccess = "file_on_success"
	CleanupLocalPathOnSuccess = "path_on_success"
	CleanupLocalTreeOnSuccess = "tree_on_success"
	// CleanupLocalAlways 无论成功、失败还是取消都清理本地源文件
	// （WebDAV 异步上传等一次性任务使用，避免失败任务堆积临时文件）。
	CleanupLocalAlways = "always"
)

type taskState struct {
	Task
	localPath      string
	conflictPolicy string
	resumePriority bool
	cancel         context.CancelFunc
	cancelMode     string
	runDone        chan struct{}
	resumeData     map[string]any
	lastEmit       time.Time
	lastProgress   int
	lastMessage    string
	speed          speedsmoother.Tracker
}

type CreateParams struct {
	ClientTaskID      string
	AccountID         int64
	AccountName       string
	DriverType        string
	FileName          string
	DisplayName       string
	SourceType        string
	SourceAccountID   int64
	SourceAccountName string
	SourceDriverType  string
	SourceFileID      string
	RelPath           string
	RelDir            string
	TargetPath        string
	TargetDisplayPath string
	LocalPath         string
	CleanupLocalMode  string
	CleanupLocalPath  string
	TotalBytes        int64
	ConflictPolicy    string
	Phase             string
}

type ServerLocalCreateParams struct {
	ClientTaskID      string
	AccountID         int64
	AccountName       string
	DriverType        string
	FileName          string
	DisplayName       string
	TargetPath        string
	TargetDisplayPath string
	LocalPath         string
	CleanupLocalMode  string
	CleanupLocalPath  string
	TotalBytes        int64
	ConflictPolicy    string
}

type BatchDeleteResult struct {
	DeletedTaskIDs []string          `json:"deleted_task_ids"`
	FailedTaskIDs  []string          `json:"failed_task_ids"`
	MissingTaskIDs []string          `json:"missing_task_ids"`
	FailedMessages map[string]string `json:"failed_messages"`
}

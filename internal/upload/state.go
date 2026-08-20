package upload

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"time"

	"litepan/pkg/timeutil"
)

// markMissingLocalFileFailed 把因本地文件缺失而无法继续的任务标记为失败。
func markMissingLocalFileFailed(st *taskState) {
	st.Status = StatusFailed
	st.Message = "上传失败"
	st.Error = "本地临时文件不存在，无法继续上传"
	st.UpdatedAt = timeutil.UnixFloat(time.Now())
}

func (m *Manager) patch(taskID string, fn func(*taskState)) {
	m.mu.Lock()
	st, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return
	}
	fn(st)
	st.UpdatedAt = timeutil.UnixFloat(time.Now())
	snap := st
	m.mu.Unlock()
	_ = m.persistTask(snap)
	m.broadcast()
}

func (m *Manager) failTask(taskID, errMsg string) {
	m.patch(taskID, func(st *taskState) {
		st.Status = StatusFailed
		st.SpeedBytesPerSecond = 0
		st.Message = "上传失败"
		st.Error = translateError(errMsg)
	})
	// always 模式（WebDAV 异步上传等一次性任务）在失败时同样清理本地源文件，
	// 避免失败/中断的上传堆积临时文件；OnSuccess 语义的任务保留文件供重试。
	m.mu.Lock()
	st, ok := m.tasks[taskID]
	m.mu.Unlock()
	if !ok || st.localPath == "" || st.CleanupLocalMode != CleanupLocalAlways {
		return
	}
	m.cleanupLocalSource(st.localPath, st.CleanupLocalPath, st.CleanupLocalMode)
}

func (m *Manager) snapshot(st *taskState) *Task {
	t := st.Task
	return &t
}

func (m *Manager) popTask(taskID string) *taskState {
	m.mu.Lock()
	st, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	cancel := st.cancel
	if cancel != nil {
		st.cancelMode = "delete"
	}
	delete(m.tasks, taskID)
	m.mu.Unlock()
	m.deletePersisted(taskID)
	if cancel != nil {
		cancel()
		select {
		case <-st.runDone:
		case <-time.After(30 * time.Second):
		}
	}
	return st
}

func newTaskID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func sortTasksDesc(tasks []Task) {
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt > tasks[j].CreatedAt
	})
}

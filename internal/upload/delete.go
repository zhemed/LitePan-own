package upload

import (
	"context"
	"strings"
)

func (m *Manager) Delete(ctx context.Context, taskID string, deleteUploadedFile bool) (bool, error) {
	m.mu.Lock()
	st, ok := m.tasks[taskID]
	m.mu.Unlock()
	if !ok {
		return false, nil
	}
	if deleteUploadedFile && st.Status == StatusSuccess {
		if err := m.deleteUploadedFile(ctx, st); err != nil {
			return true, err
		}
	}
	popped := m.popTask(taskID)
	if popped == nil {
		return false, nil
	}
	if popped.CleanupLocalMode != "" {
		m.cleanupLocalSource(popped.localPath, popped.CleanupLocalPath, popped.CleanupLocalMode)
	} else {
		m.removeLocalFile(popped.localPath)
	}
	m.broadcast()
	return true, nil
}

func (m *Manager) BatchDelete(ctx context.Context, taskIDs []string, deleteUploadedFile bool) BatchDeleteResult {
	result := BatchDeleteResult{FailedMessages: map[string]string{}}
	seen := map[string]struct{}{}
	for _, id := range taskIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		m.mu.Lock()
		st, ok := m.tasks[id]
		m.mu.Unlock()
		if !ok {
			result.MissingTaskIDs = append(result.MissingTaskIDs, id)
			continue
		}
		if deleteUploadedFile && st.Status == StatusSuccess {
			if err := m.deleteUploadedFile(ctx, st); err != nil {
				result.FailedTaskIDs = append(result.FailedTaskIDs, id)
				result.FailedMessages[id] = err.Error()
				continue
			}
		}
		popped := m.popTask(id)
		if popped == nil {
			result.MissingTaskIDs = append(result.MissingTaskIDs, id)
			continue
		}
		if popped.CleanupLocalMode != "" {
			m.cleanupLocalSource(popped.localPath, popped.CleanupLocalPath, popped.CleanupLocalMode)
		} else {
			m.removeLocalFile(popped.localPath)
		}
		result.DeletedTaskIDs = append(result.DeletedTaskIDs, id)
	}
	if len(result.DeletedTaskIDs) > 0 {
		m.broadcast()
	}
	return result
}

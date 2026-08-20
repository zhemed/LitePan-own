package guangya

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"net/http"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/pkg/strutil"
)

const (
	taskStatusDone = 2
	taskStatusFail = 3
)

const (
	downloadURLProbeRetries = 3
	downloadURLProbeBytes   = 256 << 10
	downloadURLProbeMinSize = 16 << 20
	downloadURLProbePerURL  = 2
	downloadPartSize        = 10 * 1024 * 1024
	downloadConcurrency     = 1
)

var downloadThawWaitMax = 45 * time.Second

var downloadThawWaitSchedule = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	10 * time.Second,
	10 * time.Second,
}

const defaultProbeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type downloadLink struct {
	URL        string
	Expiration time.Duration
}

func pickDownloadURL(data *downloadData) string {
	url := strings.TrimSpace(data.SignedURL)
	if url == "" {
		url = strings.TrimSpace(data.DownloadURL)
	}
	return url
}

func (d *Driver) fetchDownloadData(ctx context.Context, fileID string) (downloadData, error) {
	var data downloadData
	if err := d.apiRequest(ctx, pathDownloadURL, map[string]any{"fileId": fileID}, &data); err != nil {
		return downloadData{}, err
	}
	if pickDownloadURL(&data) == "" {
		return downloadData{}, domain.Errorf(domain.CodeDriverError, "光鸭下载地址为空")
	}
	return data, nil
}

func (d *Driver) fetchDownloadLink(ctx context.Context, fileID string, fileSize int64, thawProbe bool, ua string) (downloadLink, bool, error) {
	data, err := d.fetchDownloadData(ctx, fileID)
	if err != nil {
		return downloadLink{}, false, err
	}
	url := pickDownloadURL(&data)
	exp := data.linkExpiration()
	if !thawProbe || fileSize < downloadURLProbeMinSize {
		return downloadLink{URL: url, Expiration: exp}, true, nil
	}

	deadline := time.Now().Add(downloadThawWaitMax)
	waitIdx := 0
	ua = probeUserAgent(ua)

	for urlRound := 0; urlRound < downloadURLProbeRetries; urlRound++ {
		for !time.Now().After(deadline) {
			if d.probeDownloadRange(ctx, url, ua) {
				return downloadLink{URL: url, Expiration: exp}, true, nil
			}
			wait := thawWaitAt(waitIdx)
			waitIdx++
			if err := sleepCtx(ctx, wait); err != nil {
				return downloadLink{}, false, err
			}
		}
		if urlRound+1 >= downloadURLProbeRetries {
			break
		}
		data, err = d.fetchDownloadData(ctx, fileID)
		if err != nil {
			return downloadLink{}, false, err
		}
		url = pickDownloadURL(&data)
		exp = data.linkExpiration()
	}
	return downloadLink{URL: url, Expiration: exp}, false, nil
}

func probeUserAgent(ua string) string {
	if ua = strings.TrimSpace(ua); ua != "" {
		return ua
	}
	return defaultProbeUserAgent
}

func thawWaitAt(idx int) time.Duration {
	if idx < len(downloadThawWaitSchedule) {
		return downloadThawWaitSchedule[idx]
	}
	return 10 * time.Second
}

func (d *Driver) probeDownloadRange(ctx context.Context, url, ua string) bool {
	for i := 0; i < downloadURLProbePerURL; i++ {
		if d.probeDownloadRangeOnce(ctx, url, ua) {
			return true
		}
	}
	return false
}

func (d *Driver) probeDownloadRangeOnce(ctx context.Context, url, ua string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", downloadURLProbeBytes-1))
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return false
	}
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, downloadURLProbeBytes))
	return err == nil && n > 0
}

func (d *Driver) ResolveDownload(ctx context.Context, req driver.DownloadRequest) (*domain.DownloadInfo, error) {
	fileID := strings.TrimSpace(req.FileID)
	if fileID == "" {
		return nil, domain.Errorf(domain.CodeValidation, "file_id 不能为空")
	}

	mode := domain.DownloadRedirect
	if strings.EqualFold(strings.TrimSpace(d.add.DownloadMode), "proxy") {
		mode = domain.DownloadProxy
	}

	var size int64
	var fileName string
	if entry, err := d.fetchFileDetail(ctx, fileID); err == nil {
		if entry.ResType == 2 {
			return nil, domain.Errorf(domain.CodeValidation, "文件夹不支持下载")
		}
		size = entry.FileSize
		fileName = entry.FileName
	}

	thawProbe := size >= downloadURLProbeMinSize
	link, ready, err := d.fetchDownloadLink(ctx, fileID, size, thawProbe, req.UA)
	if err != nil {
		return nil, err
	}
	if !ready {
		if mode == domain.DownloadProxy {
			return nil, domain.Errorf(domain.CodeDriverError, "光鸭 CDN 归档解冻超时，请稍后重试")
		}
		return &domain.DownloadInfo{
			URL:         link.URL,
			Mode:        mode,
			Size:        size,
			FileName:    fileName,
			Expiration:  link.Expiration,
			ForceProxy:  true,
			ChunkSize:   downloadPartSize,
			Concurrency: downloadConcurrency,
		}, nil
	}
	return &domain.DownloadInfo{
		URL:         link.URL,
		Mode:        mode,
		Size:        size,
		FileName:    fileName,
		Expiration:  link.Expiration,
		ChunkSize:   downloadPartSize,
		Concurrency: downloadConcurrency,
	}, nil
}

func (d *Driver) deleteMode() string {
	if strings.EqualFold(strings.TrimSpace(d.add.DeleteMode), "delete") {
		return "delete"
	}
	return "trash"
}

func normalizeIDs(fileIDs []string) []string {
	out := make([]string, 0, len(fileIDs))
	for _, id := range fileIDs {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (d *Driver) waitTaskDone(ctx context.Context, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	for attempt := 0; attempt < 30; attempt++ {
		var data taskStatusData
		if err := d.apiRequest(ctx, pathTaskStatus, map[string]any{"taskId": taskID}, &data); err != nil {
			return err
		}
		switch data.Status {
		case taskStatusDone:
			return nil
		case -1, taskStatusFail:
			return domain.Errorf(domain.CodeDriverError, "光鸭任务执行失败，状态码: %d", data.Status)
		}
		if attempt < 29 {
			if err := sleepCtx(ctx, 300*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return domain.Errorf(domain.CodeDriverError, "光鸭任务执行超时")
}

func (d *Driver) deleteViaTask(ctx context.Context, fileIDs []string) error {
	var data taskData
	if err := d.apiRequest(ctx, pathDeleteFile, map[string]any{"fileIds": fileIDs}, &data); err != nil {
		return err
	}
	return d.waitTaskDone(ctx, data.TaskID)
}

func (d *Driver) moveViaTask(ctx context.Context, fileIDs []string, targetParentID string) error {
	var data taskData
	if err := d.apiRequest(ctx, pathMoveFile, map[string]any{
		"fileIds":  fileIDs,
		"parentId": targetParentID,
	}, &data); err != nil {
		return err
	}
	return d.waitTaskDone(ctx, data.TaskID)
}

func (d *Driver) listRecycleItems(ctx context.Context) ([]fileEntry, error) {
	page := 0
	var result []fileEntry
	for {
		var data listData
		if err := d.apiRequest(ctx, pathFileList, recycleListOptions(page), &data); err != nil {
			return nil, err
		}
		if len(data.List) == 0 {
			break
		}
		result = append(result, data.List...)
		if data.Total > 0 && len(result) >= data.Total {
			break
		}
		if len(data.List) < listPageSize {
			break
		}
		page++
	}
	return result, nil
}

func (d *Driver) DeleteFiles(ctx context.Context, fileIDs []string) error {
	ids := normalizeIDs(fileIDs)
	if len(ids) == 0 {
		return nil
	}
	if err := d.deleteViaTask(ctx, ids); err != nil {
		return err
	}
	if d.deleteMode() != "delete" {
		return nil
	}

	recycleMap := map[string]struct{}{}
	for attempt := 0; attempt < 6; attempt++ {
		items, err := d.listRecycleItems(ctx)
		if err != nil {
			return err
		}
		recycleMap = map[string]struct{}{}
		for _, item := range items {
			recycleMap[item.FileID] = struct{}{}
		}
		missing := false
		for _, id := range ids {
			if _, ok := recycleMap[id]; !ok {
				missing = true
				break
			}
		}
		if !missing {
			break
		}
		if attempt < 5 {
			if err := sleepCtx(ctx, 400*time.Millisecond); err != nil {
				return err
			}
		}
	}
	for _, id := range ids {
		if _, ok := recycleMap[id]; !ok {
			return domain.Errorf(domain.CodeDriverError, "已移入回收站，但未找到回收站记录: %s", id)
		}
	}
	return d.deleteViaTask(ctx, ids)
}

func (d *Driver) MoveFiles(ctx context.Context, fileIDs []string, targetParentID, _ string) error {
	ids := normalizeIDs(fileIDs)
	if len(ids) == 0 {
		return nil
	}
	target := d.resolveParent(targetParentID)
	return d.moveViaTask(ctx, ids, target)
}

func (d *Driver) CopyFiles(ctx context.Context, fileIDs []string, targetParentID string) error {
	ids := normalizeIDs(fileIDs)
	if len(ids) == 0 {
		return nil
	}
	target := d.resolveParent(targetParentID)
	var data taskData
	if err := d.apiRequest(ctx, pathCopyFile, map[string]any{
		"fileIds":  ids,
		"parentId": target,
	}, &data); err != nil {
		return err
	}
	return d.waitTaskDone(ctx, data.TaskID)
}

func (d *Driver) RenameFile(ctx context.Context, fileID, newName string) error {
	fileID = strings.TrimSpace(fileID)
	newName = strings.TrimSpace(newName)
	if fileID == "" {
		return domain.Errorf(domain.CodeValidation, "file_id 不能为空")
	}
	if newName == "" {
		return domain.Errorf(domain.CodeValidation, "新名称不能为空")
	}
	return d.apiRequest(ctx, pathRename, map[string]any{
		"fileId":  fileID,
		"newName": newName,
	}, nil)
}

func (d *Driver) CreateFolder(ctx context.Context, parentID, name string) (*domain.FileItem, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, domain.Errorf(domain.CodeValidation, "文件夹名称不能为空")
	}
	parent := d.resolveParent(parentID)
	var data createDirData
	if err := d.apiRequest(ctx, pathCreateDir, map[string]any{
		"parentId": parent,
		"dirName":  name,
	}, &data); err != nil {
		return nil, err
	}
	item := domain.FileItem{
		ID:     data.FileID,
		Name:   strutil.FirstNonEmpty(data.FileName, name),
		Size:   0,
		IsDir:  true,
		IDKind: domain.IDStable,
	}
	if data.UTime > 0 {
		item.ModTime = time.Unix(data.UTime, 0)
	}
	return &item, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

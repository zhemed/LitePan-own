package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/embyproxy"
	"litepan/internal/settings"
	"litepan/internal/strmscrape"
	"litepan/internal/upload"
)

const (
	strmScrapeFailurePolicyAllFailed = "all_failed"
	strmScrapeFailurePolicyAnyFailed = "any_failed"
	strmScrapeFailurePolicyNever     = "never"
)

func (s *Service) RunAsync(ctx context.Context, id int64, triggerSource string) (map[string]any, error) {
	res := s.submitRun(id, triggerSource, false)
	return map[string]any{
		"rule_id":        id,
		"submitted":      true,
		"trigger_source": triggerSource,
		"queued":         res.queued,
	}, nil
}

func (s *Service) runRule(id int64, triggerSource string) {
	defer s.endRun(id)
	parent := s.appCtx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()

	rule, err := s.rules.Get(ctx, id)
	if err != nil {
		s.log.Warn("automation get rule failed", "rule_id", id, "err", err)
		return
	}
	actions := decodeActions(rule.Actions)
	run := &domain.AutomationRun{
		RuleID:        id,
		TriggerSource: triggerSource,
		Status:        domain.AutomationRunRunning,
		StartedAt:     time.Now(),
		Result:        mustJSON(map[string]any{"steps": []map[string]any{}}),
	}
	runID, err := s.runs.Create(ctx, run)
	if err != nil {
		s.log.Warn("automation create run failed", "rule_id", id, "err", err)
		return
	}
	run.ID = runID

	steps := make([]map[string]any, 0, len(actions))
	previousSuccess := true
	message := "执行完成"
	status := domain.AutomationRunSuccess
	for i, action := range actions {
		step := map[string]any{
			"index":     i,
			"type":      action.Type,
			"name":      actionDisplayName(action),
			"condition": normalizedCondition(action.Condition, i),
			"status":    "skipped",
			"success":   true,
			"message":   "条件未满足，已跳过",
		}
		if shouldRunAction(action.Condition, previousSuccess, i) {
			s.setRunningStep(id, i, actionDisplayName(action), action.Type)
			runAction := action
			if action.Type == domain.AutomationActionCacheClear {
				runAction.Params = cloneMap(action.Params)
				runAction.Params["_following_actions"] = actions[i+1:]
			}
			result := s.executeAction(ctx, runAction)
			for k, v := range result {
				step[k] = v
			}
			ok, _ := step["success"].(bool)
			previousSuccess = ok
			if step["status"] == "failed" {
				status = domain.AutomationRunFailed
				if msg := strings.TrimSpace(anyString(step["message"])); msg != "" {
					message = msg
				}
			}
		}
		steps = append(steps, step)
	}
	finishedAt := time.Now()
	run.Status = status
	run.Message = message
	run.Result = mustJSON(map[string]any{"steps": steps})
	run.FinishedAt = finishedAt
	_ = s.runs.Update(ctx, run)

	rule.LastRunAt = finishedAt
	rule.LastRunStatus = status
	rule.LastRunMessage = message
	if rule.Status == domain.AutomationStatusRunning {
		if rule.TriggerType == domain.AutomationTriggerWebhook {
			rule.NextRunAt = time.Time{}
		} else if triggerSource != "schedule" && (rule.NextRunAt.IsZero() || !rule.NextRunAt.After(finishedAt)) {
			rule.NextRunAt = computeNextRun(rule.TriggerType, decodeMap(rule.TriggerConfig), finishedAt)
		}
	}
	_ = s.rules.Update(ctx, rule)
}

func (s *Service) executeAction(ctx context.Context, action RuleAction) map[string]any {
	switch action.Type {
	case domain.AutomationActionCacheClear:
		return s.runCacheClear(ctx, action.Params)
	case domain.AutomationActionDelay:
		return s.runDelay(ctx, action.Params)
	case domain.AutomationActionOrganize:
		return s.runOrganize(ctx, action.Params)
	case domain.AutomationActionStrm:
		return s.runStrm(ctx, action.Params)
	case domain.AutomationActionStrmScrape:
		return s.runStrmScrape(ctx, action.Params)
	case domain.AutomationActionEmbyRefresh:
		return s.runEmbyRefresh(ctx, action.Params)
	case domain.AutomationActionLocalUpload:
		return s.runLocalUpload(ctx, action.Params)
	default:
		return map[string]any{"status": "failed", "success": false, "message": "动作类型不支持"}
	}
}

func (s *Service) runCacheClear(ctx context.Context, params map[string]any) map[string]any {
	if s.files == nil {
		return map[string]any{"status": "failed", "success": false, "message": "文件服务未就绪"}
	}
	targets := s.collectCacheClearTargets(ctx, params["_following_actions"])
	if len(targets) == 0 {
		return map[string]any{"status": "failed", "success": false, "message": "刷新目录后面需要有整理任务或 STRM 任务"}
	}
	cleaned := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		if _, err := s.files.List(ctx, target.accountID, target.parentID, true); err != nil {
			return map[string]any{"status": "failed", "success": false, "message": err.Error()}
		}
		cleaned = append(cleaned, map[string]any{
			"account_id": target.accountID,
			"parent_id":  target.parentID,
			"path":       target.path,
		})
	}
	return map[string]any{
		"status":  "success",
		"success": true,
		"message": fmt.Sprintf("已刷新 %d 个目录", len(cleaned)),
		"data":    map[string]any{"targets": cleaned},
	}
}

func (s *Service) collectCacheClearTargets(ctx context.Context, raw any) []cacheClearTarget {
	actions, ok := raw.([]RuleAction)
	if !ok {
		return nil
	}
	targets := make([]cacheClearTarget, 0)
	seen := make(map[string]struct{})
	addTarget := func(accountID int64, parentID string, path string) {
		parentID = strings.TrimSpace(parentID)
		if accountID <= 0 || parentID == "" {
			return
		}
		key := fmt.Sprintf("%d|%s", accountID, parentID)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, cacheClearTarget{accountID: accountID, parentID: parentID, path: strings.TrimSpace(path)})
	}
	for _, action := range actions {
		switch action.Type {
		case domain.AutomationActionOrganize:
			if s.organize == nil {
				continue
			}
			taskID := strings.TrimSpace(anyString(action.Params["task_id"]))
			if taskID == "" {
				continue
			}
			task, err := s.organize.GetTask(ctx, taskID)
			if err != nil {
				continue
			}
			cfg := decodeMap(task.Config)
			accountID := task.AccountID
			if accountID <= 0 {
				accountID = int64(anyInt(cfg["account_id"]))
			}
			addTarget(accountID, anyString(cfg["target_directory_id"]), anyString(cfg["target_directory"]))
			if strings.TrimSpace(anyString(cfg["action_type"])) == "move" {
				addTarget(accountID, anyString(cfg["target_root_id"]), anyString(cfg["target_root"]))
			}
		case domain.AutomationActionStrm:
			if s.strm == nil {
				continue
			}
			taskID := int64(anyInt(action.Params["task_id"]))
			if taskID <= 0 {
				continue
			}
			task, err := s.strm.GetTask(ctx, taskID)
			if err != nil {
				continue
			}
			addTarget(task.AccountID, task.ParentID, task.Path)
		}
	}
	return targets
}

func (s *Service) runDelay(ctx context.Context, params map[string]any) map[string]any {
	seconds := clampInt(anyInt(params["seconds"]), 1, 24*3600)
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return map[string]any{"status": "failed", "success": false, "message": "等待被取消"}
	case <-timer.C:
		return map[string]any{"status": "success", "success": true, "message": fmt.Sprintf("已等待 %d 秒", seconds), "data": map[string]any{"seconds": seconds}}
	}
}

func (s *Service) runOrganize(ctx context.Context, params map[string]any) map[string]any {
	taskID := strings.TrimSpace(anyString(params["task_id"]))
	if taskID == "" {
		return map[string]any{"status": "failed", "success": false, "message": "未选择整理任务"}
	}
	task, err := s.organize.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	if s.organize.IsRunning(taskID) {
		return map[string]any{"status": "failed", "success": false, "message": "整理任务正在执行中"}
	}
	startedAt := time.Now()
	if _, err := s.organize.RunTask(ctx, taskID); err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	for s.organize.IsRunning(taskID) {
		select {
		case <-ctx.Done():
			return map[string]any{"status": "failed", "success": false, "message": "整理任务等待被取消"}
		case <-time.After(2 * time.Second):
		}
	}
	updated, err := s.organize.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	summary := decodeMap(updated.LastRunResult)
	fresh := !updated.LastRunAt.IsZero() && !updated.LastRunAt.Before(startedAt.Add(-time.Second))
	outcome := evaluateOrganizeAction(summary, params, fresh && updated.Status != domain.MediaOrganizeStatusError)
	return map[string]any{
		"status":  ternaryStatus(outcome.success),
		"success": outcome.success,
		"message": outcome.message,
		"data": map[string]any{
			"task_id":          task.ID,
			"name":             task.TaskName,
			"summary":          summary,
			"risk_percent":     outcome.riskPercent,
			"max_risk_percent": outcome.maxRiskPercent,
			"abnormal_skipped": outcome.abnormalSkipped,
			"normal_skipped":   outcome.normalSkipped,
			"risk_total":       outcome.riskTotal,
		},
	}
}

type organizeActionOutcome struct {
	success         bool
	message         string
	riskPercent     float64
	maxRiskPercent  int
	abnormalSkipped int
	normalSkipped   int
	riskTotal       int
}

func evaluateOrganizeAction(summary, params map[string]any, runCompleted bool) organizeActionOutcome {
	total := max(0, anyInt(summary["total"]))
	failed := max(0, anyInt(summary["failed"]))
	skipped := max(0, anyInt(summary["skipped"]))
	normalSkipped := max(0, anyInt(summary["normal_skipped"]))
	abnormalSkipped := skipped
	if summary["abnormal_skipped"] != nil {
		abnormalSkipped = max(0, anyInt(summary["abnormal_skipped"]))
	}
	riskTotal := max(0, total-normalSkipped)
	maxRisk := 30
	if params["max_risk_percent"] != nil {
		maxRisk = clampInt(anyInt(params["max_risk_percent"]), 0, 100)
	}
	risk := 0.0
	if riskTotal > 0 {
		risk = math.Round(float64(failed+abnormalSkipped)/float64(riskTotal)*10000) / 100
	}
	stopped, _ := summary["stopped"].(bool)
	success := runCompleted && !stopped && failed == 0 && risk <= float64(maxRisk)
	message := "整理完成，异常比例 " + strconv.FormatFloat(risk, 'f', -1, 64) + "%"
	switch {
	case !runCompleted:
		message = "整理任务未正常完成"
	case stopped:
		message = "整理任务已停止"
	case failed > 0:
		message = fmt.Sprintf("整理存在失败项：%d 个", failed)
	case risk > float64(maxRisk):
		message = fmt.Sprintf("整理异常比例 %s%% 超过允许值 %d%%", strconv.FormatFloat(risk, 'f', -1, 64), maxRisk)
	}
	return organizeActionOutcome{
		success:         success,
		message:         message,
		riskPercent:     risk,
		maxRiskPercent:  maxRisk,
		abnormalSkipped: abnormalSkipped,
		normalSkipped:   normalSkipped,
		riskTotal:       riskTotal,
	}
}

func (s *Service) runStrm(ctx context.Context, params map[string]any) map[string]any {
	taskID := int64(anyInt(params["task_id"]))
	if taskID <= 0 {
		return map[string]any{"status": "failed", "success": false, "message": "未选择 STRM 任务"}
	}
	if _, err := s.strm.GetTask(ctx, taskID); err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	startedAt := time.Now()
	runMode := strings.TrimSpace(anyString(params["run_mode"]))
	if runMode == "" {
		runMode = domain.StrmRunModeAuto
	}
	if _, err := s.strm.RunTaskNow(ctx, taskID, runMode); err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	for s.strm.IsTaskRunning(taskID) {
		select {
		case <-ctx.Done():
			return map[string]any{"status": "failed", "success": false, "message": "STRM 任务等待被取消"}
		case <-time.After(2 * time.Second):
		}
	}
	updated, err := s.strm.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	success := !updated.LastScan.IsZero() && !updated.LastScan.Before(startedAt.Add(-time.Second)) && updated.LastScanStatus != "failed"
	message := "STRM 任务执行完成"
	if !success {
		if msg := strings.TrimSpace(updated.ErrorMessage); msg != "" {
			message = msg
		} else {
			message = "STRM 任务执行失败"
		}
	}
	return map[string]any{
		"status":  ternaryStatus(success),
		"success": success,
		"message": message,
		"data": map[string]any{
			"task_id":          updated.ID,
			"name":             updated.Name,
			"last_scan_status": updated.LastScanStatus,
		},
	}
}

func (s *Service) runStrmScrape(ctx context.Context, params map[string]any) map[string]any {
	if s.strmScrape == nil {
		return map[string]any{"status": "failed", "success": false, "message": "STRM 刮削服务未就绪"}
	}
	taskID := int64(anyInt(params["task_id"]))
	if taskID <= 0 {
		return map[string]any{"status": "failed", "success": false, "message": "未选择 STRM 任务"}
	}
	task, err := s.strm.GetTask(ctx, taskID)
	if err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	req := strmscrape.RunRequest{
		StrmTaskID: taskID,
		WriteMode:  strings.TrimSpace(anyString(params["write_mode"])),
	}
	if err := s.strmScrape.RunAsync(ctx, req); err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	for {
		progress := s.strmScrape.GetProgress()
		if !progress.Running {
			policy := normalizeStrmScrapeFailurePolicy(params["failure_policy"])
			status, success := strmScrapeOutcome(progress, policy)
			message := strings.TrimSpace(progress.Message)
			if message == "" {
				if strings.TrimSpace(progress.Error) == "" {
					message = "本地 STRM 元数据生成完成"
				} else {
					message = "本地 STRM 元数据生成失败"
				}
			}
			if errMsg := strings.TrimSpace(progress.Error); errMsg != "" {
				message = errMsg
			} else if status == "partial" {
				message = fmt.Sprintf("%s（按设置继续联动）", message)
			}
			return map[string]any{
				"status":  status,
				"success": success,
				"message": message,
				"data": map[string]any{
					"task_id":        task.ID,
					"name":           task.Name,
					"total":          progress.Total,
					"done":           progress.Done,
					"skipped":        progress.Skipped,
					"failed":         progress.Failed,
					"failure_policy": policy,
				},
			}
		}
		select {
		case <-ctx.Done():
			return map[string]any{"status": "failed", "success": false, "message": "本地 STRM 元数据生成等待被取消"}
		case <-time.After(2 * time.Second):
		}
	}
}

func normalizeStrmScrapeFailurePolicy(value any) string {
	switch strings.TrimSpace(anyString(value)) {
	case strmScrapeFailurePolicyAnyFailed:
		return strmScrapeFailurePolicyAnyFailed
	case strmScrapeFailurePolicyNever:
		return strmScrapeFailurePolicyNever
	default:
		return strmScrapeFailurePolicyAllFailed
	}
}

func strmScrapeOutcome(progress strmscrape.Progress, policy string) (string, bool) {
	if strings.TrimSpace(progress.Error) != "" {
		return "failed", false
	}
	if progress.Failed <= 0 {
		return "success", true
	}
	allFailed := progress.Done-progress.Failed <= 0
	if policy == strmScrapeFailurePolicyAnyFailed ||
		(policy == strmScrapeFailurePolicyAllFailed && allFailed) {
		return "failed", false
	}
	return "partial", true
}

func (s *Service) runEmbyRefresh(ctx context.Context, params map[string]any) map[string]any {
	if s.emby == nil {
		return map[string]any{"status": "failed", "success": false, "message": "Emby 服务未就绪"}
	}
	req := embyproxy.RefreshRequest{
		ConfigID:  strings.TrimSpace(anyString(params["emby_id"])),
		Mode:      strings.TrimSpace(anyString(params["mode"])),
		LibraryID: strings.TrimSpace(anyString(params["library_id"])),
	}
	result, err := s.emby.RefreshLibrary(ctx, req)
	if err != nil {
		return map[string]any{"status": "failed", "success": false, "message": err.Error()}
	}
	message := "已通知 Emby 刷库"
	if result.Mode == "library" && result.LibraryName != "" {
		message = "已通知 Emby 扫描媒体库：" + result.LibraryName
	}
	return map[string]any{
		"status":  "success",
		"success": true,
		"message": message,
		"data": map[string]any{
			"emby_id":      result.ConfigID,
			"emby_name":    result.ConfigName,
			"mode":         result.Mode,
			"task_id":      result.TaskID,
			"library_id":   result.LibraryID,
			"library_name": result.LibraryName,
		},
	}
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func loadLocalUploadState(dataDir, mapping string) map[string]string {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(mapping) == "" {
		return make(map[string]string)
	}
	safe := strings.ReplaceAll(strings.TrimSpace(mapping), "/", "_")
	safe = strings.ReplaceAll(safe, "\\", "_")
	fpath := filepath.Join(dataDir, "local_upload_state_"+safe+".json")
	data, err := os.ReadFile(fpath)
	if err != nil {
		return make(map[string]string)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]string)
	}
	if m == nil {
		m = make(map[string]string)
	}
	return m
}

func saveLocalUploadState(dataDir, mapping string, state map[string]string) {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(mapping) == "" {
		return
	}
	safe := strings.ReplaceAll(strings.TrimSpace(mapping), "/", "_")
	safe = strings.ReplaceAll(safe, "\\", "_")
	fpath := filepath.Join(dataDir, "local_upload_state_"+safe+".json")
	data, _ := json.Marshal(state)
	_ = os.WriteFile(fpath, data, 0644)
}

func (s *Service) runLocalUpload(ctx context.Context, params map[string]any) map[string]any {
	if s.uploads == nil {
		return map[string]any{"status": "failed", "success": false, "message": "上传服务未就绪"}
	}
	if s.settings == nil {
		return map[string]any{"status": "failed", "success": false, "message": "设置服务未就绪"}
	}
	if s.files == nil {
		return map[string]any{"status": "failed", "success": false, "message": "文件服务未就绪"}
	}
	accountID := int64(anyInt(params["account_id"]))
	if accountID <= 0 {
		return map[string]any{"status": "failed", "success": false, "message": "未选择目标网盘账号"}
	}
	// 支持多选 mappings，兼容单 mapping
	var mappingNames []string
	if rawArr, ok := params["mappings"]; ok {
		if arr, ok := rawArr.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					mappingNames = append(mappingNames, strings.TrimSpace(s))
				}
			}
		} else if s, ok := rawArr.(string); ok && strings.TrimSpace(s) != "" {
			mappingNames = append(mappingNames, strings.TrimSpace(s))
		}
	}
	if len(mappingNames) == 0 {
		if m := strings.TrimSpace(anyString(params["mapping"])); m != "" {
			mappingNames = append(mappingNames, m)
		} else if m := strings.TrimSpace(anyString(params["mapping_name"])); m != "" {
			mappingNames = append(mappingNames, m)
		}
	}
	if len(mappingNames) == 0 {
		return map[string]any{"status": "failed", "success": false, "message": "未选择本地映射目录"}
	}
	targetParent := strings.TrimSpace(anyString(params["target_parent_id"]))
	if targetParent == "" {
		targetParent = strings.TrimSpace(anyString(params["target_path"]))
	}
	targetDisplay := strings.TrimSpace(anyString(params["target_display_path"]))
	conflict := strings.TrimSpace(anyString(params["conflict_policy"]))
	if conflict == "" {
		conflict = "overwrite"
	}
	sourceSubPath := strings.TrimSpace(anyString(params["source_path"]))
	if sourceSubPath == "" {
		sourceSubPath = strings.TrimSpace(anyString(params["path"]))
	}
	raw := s.settings.String(settings.KeyLocalUploadMappings)
	var mappings []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &mappings); err != nil {
		return map[string]any{"status": "failed", "success": false, "message": "读取映射配置失败"}
	}
	// 循环处理多个映射，汇总结果
	var totalScanned, totalCreated, totalSkippedByHash, totalSkipped int
	var allMsgs []string
	for _, mappingName := range mappingNames {
		var mappingPath string
		for _, m := range mappings {
			if strings.TrimSpace(m.Name) == mappingName {
				mappingPath = strings.TrimSpace(m.Path)
				break
			}
		}
		if mappingPath == "" {
			allMsgs = append(allMsgs, fmt.Sprintf("映射不存在：%s", mappingName))
			continue
		}
		mappingPath = filepath.Clean(mappingPath)
		sourceRoot := mappingPath
		if sourceSubPath != "" {
			clean := filepath.Clean(filepath.Join(mappingPath, filepath.FromSlash(sourceSubPath)))
			if !strings.HasPrefix(clean, mappingPath) && clean != mappingPath {
				allMsgs = append(allMsgs, fmt.Sprintf("源路径超出映射范围：%s", mappingName))
				continue
			}
			sourceRoot = clean
		}
	info, err := os.Stat(sourceRoot)
	if err != nil {
		return map[string]any{"status": "failed", "success": false, "message": fmt.Sprintf("源目录不存在：%v", err)}
	}
	type src struct {
		abs     string
		relPath string
		relDir  string
	}
	var allSources []src
	if !info.IsDir() {
		rel := filepath.Base(sourceRoot)
		allSources = append(allSources, src{abs: sourceRoot, relPath: rel, relDir: ""})
	} else {
		err = filepath.WalkDir(sourceRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			lower := strings.ToLower(name)
			if d.IsDir() {
				if lower == "__macosx" || lower == ".spotlight-v100" || lower == ".trashes" || lower == ".fseventsd" || lower == "$recycle.bin" || lower == "system volume information" {
					return filepath.SkipDir
				}
				return nil
			}
			if lower == ".ds_store" || lower == "thumbs.db" || lower == "desktop.ini" || strings.HasPrefix(lower, "._") {
				return nil
			}
			rel, _ := filepath.Rel(sourceRoot, p)
			rel = filepath.ToSlash(rel)
			relDir := ""
			if dir := filepath.Dir(rel); dir != "." {
				relDir = dir
			}
			allSources = append(allSources, src{abs: p, relPath: rel, relDir: relDir})
			return nil
		})
		if err != nil {
			return map[string]any{"status": "failed", "success": false, "message": fmt.Sprintf("遍历目录失败：%v", err)}
		}
	}
	if len(allSources) == 0 {
		allMsgs = append(allMsgs, fmt.Sprintf("[%s] 空目录", mappingName))
		continue
	}
	if targetParent == "" {
		return map[string]any{"status": "failed", "success": false, "message": "未指定网盘目标目录"}
	}
	// 增量：全量 hash + 云端存在双重检查
	state := loadLocalUploadState(s.dataDir, mappingName)
	newState := make(map[string]string, len(state))
	for k, v := range state {
		newState[k] = v
	}
	// 为云端存在检查准备：先定义 ensureDir 供复用
	targetDirsForCheck := map[string]string{"": targetParent}
	ensureDirForCheck := func(relDir string) (string, error) {
		relDir = strings.Trim(strings.ReplaceAll(relDir, "\\", "/"), "/")
		if relDir == "" {
			return targetParent, nil
		}
		if cached, ok := targetDirsForCheck[relDir]; ok {
			return cached, nil
		}
		cur := targetParent
		parts := strings.Split(relDir, "/")
		key := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			if key == "" {
				key = part
			} else {
				key = key + "/" + part
			}
			if cached, ok := targetDirsForCheck[key]; ok {
				cur = cached
				continue
			}
			items, err := s.files.List(ctx, accountID, cur, false)
			if err != nil {
				return "", err
			}
			next := ""
			for _, item := range items {
				if item.IsDir && item.Name == part {
					next = item.ID
					break
				}
			}
			if next == "" {
				// 目标子目录不存在，说明云端该路径下肯定没有文件，直接返回 cur，让后续 List 判不存在
				return cur, nil
			}
			cur = next
			targetDirsForCheck[key] = cur
		}
		return cur, nil
	}
	var sources []src
	var skippedByHash int
	for _, sc := range allSources {
		if err := ctx.Err(); err != nil {
			return map[string]any{"status": "failed", "success": false, "message": "任务被取消"}
		}
		h, err := fileHash(sc.abs)
		if err != nil {
			sources = append(sources, sc)
			continue
		}
		if oldHash, ok := state[sc.relPath]; ok && oldHash == h {
			// 本地未变，再查云端在不在，不在则重传
			parentID, err := ensureDirForCheck(filepath.Join(mappingName, sc.relDir))
			if err == nil {
				if items, err := s.files.List(ctx, accountID, parentID, false); err == nil {
					exists := false
					for _, item := range items {
						if !item.IsDir && item.Name == filepath.Base(sc.abs) {
							exists = true
							break
						}
					}
					if exists {
						skippedByHash++
						newState[sc.relPath] = h
						continue
					}
					// 云端不存在， fallthrough 到重传
				} else {
					skippedByHash++
					newState[sc.relPath] = h
					continue
				}
			} else {
				skippedByHash++
				newState[sc.relPath] = h
				continue
			}
		}
		newState[sc.relPath] = h
		sources = append(sources, sc)
	}
	if len(sources) == 0 {
		saveLocalUploadState(s.dataDir, mappingName, newState)
		allMsgs = append(allMsgs, fmt.Sprintf("[%s] 增量跳过 %d 个", mappingName, skippedByHash))
		totalScanned += len(allSources)
		totalSkippedByHash += skippedByHash
		continue
	}
	const batchSize = 100
	batch := make([]upload.CreateParams, 0, batchSize)
	targetDirs := map[string]string{"": targetParent}
	ensureDir := func(relDir string) (string, error) {
		relDir = strings.Trim(strings.ReplaceAll(relDir, "\\", "/"), "/")
		if relDir == "" {
			return targetParent, nil
		}
		if cached, ok := targetDirs[relDir]; ok {
			return cached, nil
		}
		cur := targetParent
		parts := strings.Split(relDir, "/")
		key := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			if key == "" {
				key = part
			} else {
				key = key + "/" + part
			}
			if cached, ok := targetDirs[key]; ok {
				cur = cached
				continue
			}
			items, err := s.files.List(ctx, accountID, cur, false)
			if err != nil {
				return "", err
			}
			next := ""
			for _, item := range items {
				if item.IsDir && item.Name == part {
					next = item.ID
					break
				}
			}
			if next == "" {
				created, err := s.files.CreateFolder(ctx, accountID, cur, part)
				if err != nil {
					return "", err
				}
				next = created.ID
			}
			cur = next
			targetDirs[key] = cur
		}
		return cur, nil
	}
	var createdCount int
	var skipped int
	var firstErr error
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		created, err := s.uploads.CreateBatch(ctx, batch)
		if err != nil {
			return err
		}
		createdCount += len(created)
		batch = batch[:0]
		return nil
	}
	for _, sc := range sources {
		if err := ctx.Err(); err != nil {
			return map[string]any{"status": "failed", "success": false, "message": "任务被取消"}
		}
		parent, err := ensureDir(filepath.Join(mappingName, sc.relDir))
		if err != nil {
			skipped++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		fi, err := os.Stat(sc.abs)
		if err != nil {
			skipped++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		batch = append(batch, upload.CreateParams{
			AccountID:         accountID,
			FileName:          filepath.Base(sc.abs),
			TargetPath:        parent,
			TargetDisplayPath: func() string {
				base := strings.Trim(targetDisplay, "/")
				rel := strings.Trim(filepath.Join(mappingName, sc.relDir), "/")
				if rel == "" {
					return base
				}
				if base == "" {
					return rel
				}
				return base + "/" + rel
			}(),
			LocalPath:      sc.abs,
			TotalBytes:     fi.Size(),
			ConflictPolicy: conflict,
			SourceType:     upload.SourceTypeServerLocal,
		})
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return map[string]any{"status": "failed", "success": false, "message": fmt.Sprintf("创建上传任务失败：%v", err)}
			}
		}
	}
	if err := flush(); err != nil {
		return map[string]any{"status": "failed", "success": false, "message": fmt.Sprintf("创建上传任务失败：%v", err)}
	}
	// 只有全部成功才更新 state
	if skipped == 0 {
		saveLocalUploadState(s.dataDir, mappingName, newState)
	}
	msg := fmt.Sprintf("[%s] 已创建 %d 个", mappingName, createdCount)
	if skippedByHash > 0 {
		msg += fmt.Sprintf("，跳过 %d", skippedByHash)
	}
	if skipped > 0 {
		msg += fmt.Sprintf("，%d 失败：%v", skipped, firstErr)
	}
	allMsgs = append(allMsgs, msg)
		totalScanned += len(allSources)
		totalCreated += createdCount
		totalSkippedByHash += skippedByHash
		totalSkipped += skipped
	}
	// 汇总
	if len(allMsgs) > 0 && totalCreated == 0 && totalSkipped == 0 {
		// 全部跳过的情况
		return map[string]any{"status": "success", "success": true, "message": "增量检查：" + fmt.Sprintf("%d 个文件均未变化，已跳过", totalSkippedByHash), "data": map[string]any{"scanned": totalScanned, "created": 0, "skipped": totalSkippedByHash}}
	}
	msgTotal := fmt.Sprintf("共 %d 个映射，已创建 %d 个上传任务", len(mappingNames), totalCreated)
	if totalSkippedByHash > 0 {
		msgTotal += fmt.Sprintf("，跳过 %d 个未变化", totalSkippedByHash)
	}
	if len(allMsgs) > 0 {
		msgTotal += "；" + strings.Join(allMsgs, "；")
	}
	if totalSkipped > 0 {
		return map[string]any{"status": "failed", "success": false, "message": msgTotal, "data": map[string]any{"created": totalCreated, "scanned": totalScanned, "skipped": totalSkipped, "skippedByHash": totalSkippedByHash}}
	}
	return map[string]any{"status": "success", "success": true, "message": msgTotal, "data": map[string]any{"created": totalCreated, "scanned": totalScanned, "skippedByHash": totalSkippedByHash}}
}

type submitRunResult struct {
	queued bool
}

func (s *Service) submitRun(ruleID int64, triggerSource string, dedupe bool) submitRunResult {
	s.mu.Lock()
	if dedupe && s.pendingCount[ruleID] > 0 {
		s.mu.Unlock()
		return submitRunResult{queued: true}
	}
	if s.runningRuleID != 0 {
		s.pendingRuns = append(s.pendingRuns, queuedRun{ruleID: ruleID, triggerSource: triggerSource})
		s.pendingCount[ruleID]++
		s.mu.Unlock()
		return submitRunResult{queued: true}
	}
	s.runningRuleID = ruleID
	s.mu.Unlock()
	go s.runRule(ruleID, triggerSource)
	return submitRunResult{queued: false}
}

func (s *Service) endRun(ruleID int64) {
	var next *queuedRun
	s.mu.Lock()
	if s.runningRuleID == ruleID {
		if len(s.pendingRuns) > 0 {
			queued := s.pendingRuns[0]
			s.pendingRuns = s.pendingRuns[1:]
			if s.pendingCount[queued.ruleID] > 1 {
				s.pendingCount[queued.ruleID]--
			} else {
				delete(s.pendingCount, queued.ruleID)
			}
			s.runningRuleID = queued.ruleID
			next = &queued
		} else {
			s.runningRuleID = 0
		}
	}
	delete(s.runningStep, ruleID)
	s.mu.Unlock()
	if next != nil {
		go s.runRule(next.ruleID, next.triggerSource)
	}
}

func (s *Service) setRunningStep(ruleID int64, index int, name, actionType string) {
	s.mu.Lock()
	s.runningStep[ruleID] = map[string]any{
		"index": index,
		"name":  name,
		"type":  actionType,
	}
	s.mu.Unlock()
}

func normalizedCondition(v string, index int) string {
	cond := strings.TrimSpace(v)
	if index == 0 {
		return domain.AutomationConditionAlways
	}
	switch cond {
	case domain.AutomationConditionAlways, domain.AutomationConditionPrevSuccess, domain.AutomationConditionPrevFailed:
		return cond
	default:
		return domain.AutomationConditionPrevSuccess
	}
}

func shouldRunAction(condition string, previousSuccess bool, index int) bool {
	switch normalizedCondition(condition, index) {
	case domain.AutomationConditionAlways:
		return true
	case domain.AutomationConditionPrevFailed:
		return !previousSuccess
	default:
		return previousSuccess
	}
}

func actionDisplayName(action RuleAction) string {
	if name := strings.TrimSpace(action.Name); name != "" {
		return name
	}
	switch action.Type {
	case domain.AutomationActionDelay:
		return "等待"
	case domain.AutomationActionOrganize:
		return "目录整理"
	case domain.AutomationActionStrm:
		return "STRM 任务"
	case domain.AutomationActionStrmScrape:
		return "生成本地 STRM 元数据"
	case domain.AutomationActionCacheClear:
		return "刷新目录"
	case domain.AutomationActionEmbyRefresh:
		return "Emby 刷库"
	case domain.AutomationActionLocalUpload:
		return "本地上传"
	default:
		return action.Type
	}
}

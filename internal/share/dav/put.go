package dav

import (
	"context"
	"fmt"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"litepan/internal/domain"
	"litepan/internal/upload"
)

type uploadPlan struct {
	accountID   int64
	accountName string
	parentID    string
	fileName    string
	existed     bool
	noop        bool
}

func (fs *FileSystem) planUpload(ctx context.Context, webPath string, exclusive bool) (*uploadPlan, error) {
	parsed := ParseWebDAVPath(webPath)
	if parsed.AccountName == "" || len(parsed.RelParts) == 0 {
		return nil, os.ErrPermission
	}
	if isMacOSMetadataPath(append([]string{parsed.AccountName}, parsed.RelParts...)) {
		return &uploadPlan{noop: true}, nil
	}
	acc, err := fs.resolver.accountByName(ctx, parsed.AccountName)
	if err != nil {
		return nil, err
	}
	fileName := parsed.RelParts[len(parsed.RelParts)-1]
	parentParts := parsed.RelParts[:len(parsed.RelParts)-1]
	parentID := "0"
	if len(parentParts) > 0 {
		parentItem, _, err := fs.resolver.resolveUnderAccount(ctx, acc.ID, parentParts)
		if err != nil {
			return nil, err
		}
		if !parentItem.IsDir {
			return nil, os.ErrInvalid
		}
		parentID = parentItem.ID
	}
	existed := false
	if cur, _, err := fs.resolver.resolveUnderAccount(ctx, acc.ID, parsed.RelParts); err == nil {
		if cur.IsDir {
			return nil, errUploadToCollection
		}
		existed = true
		if exclusive {
			return nil, os.ErrExist
		}
	}
	return &uploadPlan{
		accountID:   acc.ID,
		accountName: acc.Name,
		parentID:    parentID,
		fileName:    fileName,
		existed:     existed,
	}, nil
}

var errUploadToCollection = errors.New("cannot overwrite a collection with PUT")

func (s *Server) servePut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	webPath := resourcePath(r)
	exclusive := r.Header.Get("If-None-Match") == "*"
	// 调试：打印 PUT 的全部请求头（确认飞牛到底携带哪些字段）。
	s.log.Warn("webdav put all headers", "path", webPath, "headers", fmt.Sprintf("%v", r.Header))

	plan, err := s.fs.planUpload(ctx, webPath, exclusive)
	if err != nil {
		writeUploadErr(w, err)
		return
	}
	if plan.noop {
		w.WriteHeader(http.StatusCreated)
		return
	}

	// 增量跳过（仅大文件，避免小文件的列表 API 风暴）：目标网盘已有同名同大小 → 201。
	if r.ContentLength > 512*1024*1024 && s.fs.files.ExistsByNameAndSize(ctx, plan.accountID, plan.parentID, plan.fileName, r.ContentLength) {
		_, _ = io.Copy(io.Discard, r.Body)
		s.log.Warn("webdav put skip existing", "path", webPath, "size", r.ContentLength)
		if plan.existed {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusCreated)
		return
	}

	// 同机本地源：PUT 目标第一段目录名匹配本地源映射且本地文件存在（大小一致）
	// → 读丢弃请求体、立即 201、后台任务直接读本地源上传（两遍本地读，零落盘）。
	if len(s.localSources) > 0 {
		parsed := ParseWebDAVPath(webPath)
		if len(parsed.RelParts) > 1 {
			if base, ok := s.localSources[parsed.RelParts[0]]; ok {
				localPath := filepath.Join(append([]string{base}, parsed.RelParts[1:]...)...)
				if info, statErr := os.Stat(localPath); statErr == nil && !info.IsDir() && r.ContentLength >= 0 && info.Size() == r.ContentLength {
					_, _ = io.Copy(io.Discard, r.Body)
					if _, err := s.uploads.CreateServerLocalTask(ctx, upload.ServerLocalCreateParams{
						AccountID:         plan.accountID,
						AccountName:       plan.accountName,
						FileName:          plan.fileName,
						DisplayName:       plan.fileName,
						TargetPath:        plan.parentID,
						TargetDisplayPath: webPath,
						LocalPath:         localPath,
						TotalBytes:        info.Size(),
						ConflictPolicy:    "overwrite",
						CleanupLocalMode:  upload.CleanupLocalNone, // 本地源不清理（飞牛的文件）
					}); err != nil {
						s.log.Warn("webdav local source enqueue", "path", webPath, "err", err)
						http.Error(w, "Internal Server Error", http.StatusInternalServerError)
						return
					}
					s.log.Warn("webdav put local source", "path", webPath, "local", localPath, "size", info.Size())
					if plan.existed {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					w.WriteHeader(http.StatusCreated)
					return
				}
			}
		}
	}

	// 普通路径（非本地源）：收 body 到临时文件 → 后台任务上传。
	// v35 删流式链时此处曾漏接 servePutAsync，导致所有普通 PUT 静默 200 空响应、文件丢失。
	s.servePutAsync(w, r, plan, webPath)
}

func (s *Server) servePutAsync(w http.ResponseWriter, r *http.Request, plan *uploadPlan, webPath string) {
	ctx := r.Context()
	tmp, tmpPath, release, err := createWebDAVTempFile(s.fs.dataDir, plan.fileName, s.fs.tempRegistry)
	if err != nil {
		s.log.Warn("webdav put temp file", "path", webPath, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(tmp, r.Body); err != nil {
		_ = tmp.Close()
		release()
		s.log.Warn("webdav put read body", "path", webPath, "err", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := tmp.Close(); err != nil {
		release()
		s.log.Warn("webdav put close temp", "path", webPath, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		release()
		s.log.Warn("webdav put stat temp", "path", webPath, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	parsed := ParseWebDAVPath(webPath)
	if info.Size() == 0 {
		if _, staging := stripWebDAVStagingSuffix(plan.fileName); staging {
			s.fs.resolver.rememberFile(ctx, plan.accountID, parsed.RelParts, plan.parentID, domain.FileItem{
				Name: plan.fileName,
				Size: 0,
			})
		}
		release()
		if plan.existed {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		return
	}

	// 回退（驱动不支持流式或未知大小）：异步化——收完数据立即响应，网盘上传转后台任务执行。
	// 复用 CreateServerLocalTask（持久化、断点续传、失败重试、完成后自动清理临时文件），
	// 避免客户端（如 fnOS 备份）等待网盘上传完成而触发读超时。
	// 注意：任务系统的 TargetPath 是目标文件夹 ID（worker 直接用作 ParentID），
	// 不是 WebDAV 路径——这里传 planUpload 解析出的父目录 ID。
	if _, err := s.uploads.CreateServerLocalTask(ctx, upload.ServerLocalCreateParams{
		AccountID:         plan.accountID,
		AccountName:       plan.accountName,
		FileName:          plan.fileName,
		DisplayName:       plan.fileName,
		TargetPath:        plan.parentID,
		TargetDisplayPath: webPath,
		LocalPath:         tmpPath,
		TotalBytes:        info.Size(),
		ConflictPolicy:    "overwrite",
		CleanupLocalMode:  "always",
	}); err != nil {
		release()
		s.log.Warn("webdav put enqueue", "path", webPath, "account", plan.accountID, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if plan.existed {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func writeUploadErr(w http.ResponseWriter, err error) {
	if errors.Is(err, errUploadToCollection) {
		http.Error(w, err.Error(), http.StatusMethodNotAllowed)
		return
	}
	if errors.Is(err, os.ErrPermission) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if errors.Is(err, os.ErrExist) {
		http.Error(w, "File already exists", http.StatusPreconditionFailed)
		return
	}
	if errors.Is(err, os.ErrInvalid) {
		http.Error(w, "Parent path is not a collection", http.StatusConflict)
		return
	}
	if ae, ok := domain.AsAppError(err); ok {
		http.Error(w, ae.Message, ae.HTTPStatus())
		return
	}
	if os.IsNotExist(err) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	http.Error(w, "Upload failed", http.StatusConflict)
}

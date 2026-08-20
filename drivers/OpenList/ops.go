package openlist

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/httpx"
)

const downloadURLExpiration = 2 * time.Hour

func (d *Driver) CreateFolder(ctx context.Context, parentID, name string) (*domain.FileItem, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, domain.Errorf(domain.CodeValidation, "文件夹名称不能为空")
	}
	target := joinPath(d.normalizePath(parentID), name)
	if err := d.apiRequest(ctx, http.MethodPost, "/fs/mkdir", mkdirReq{Path: target}, nil, nil); err != nil {
		return nil, err
	}
	return &domain.FileItem{ID: target, Name: name, IsDir: true, IDKind: domain.IDPath}, nil
}

func (d *Driver) RenameFile(ctx context.Context, fileID, newName string) error {
	p := d.normalizePath(fileID)
	name := strings.TrimSpace(newName)
	if p == "/" {
		return domain.Errorf(domain.CodeValidation, "根目录不支持重命名")
	}
	if name == "" {
		return domain.Errorf(domain.CodeValidation, "新名称不能为空")
	}
	return d.apiRequest(ctx, http.MethodPost, "/fs/rename", renameReq{Path: p, Name: name}, nil, nil)
}

func (d *Driver) DeleteFiles(ctx context.Context, fileIDs []string) error {
	groups, err := groupByParent(fileIDs, "删除")
	if err != nil {
		return err
	}
	for dir, names := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.apiRequest(ctx, http.MethodPost, "/fs/remove", removeReq{Dir: dir, Names: names}, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) MoveFiles(ctx context.Context, fileIDs []string, targetParentID, _ string) error {
	return d.batchDirOp(ctx, fileIDs, targetParentID, "/fs/move", "移动")
}

func (d *Driver) CopyFiles(ctx context.Context, fileIDs []string, targetParentID string) error {
	return d.batchDirOp(ctx, fileIDs, targetParentID, "/fs/copy", "复制")
}

// batchDirOp 按来源目录分组执行移动/复制（OpenList 的 move/copy 一次只能操作同一目录下的项）。
func (d *Driver) batchDirOp(ctx context.Context, fileIDs []string, targetParentID, apiPath, verb string) error {
	groups, err := groupByParent(fileIDs, verb)
	if err != nil {
		return err
	}
	dst := d.normalizePath(targetParentID)
	for src, names := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.apiRequest(ctx, http.MethodPost, apiPath, dirOpReq{SrcDir: src, DstDir: dst, Names: names}, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// groupByParent 把文件 ID 按父目录分组；根目录本身不允许操作。
func groupByParent(fileIDs []string, verb string) (map[string][]string, error) {
	groups := make(map[string][]string)
	for _, id := range fileIDs {
		p := strings.TrimSpace(id)
		if p == "" {
			continue
		}
		if p == "/" {
			return nil, domain.Errorf(domain.CodeValidation, "根目录不支持%s", verb)
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		p = path.Clean(p)
		dir, name := path.Split(p)
		groups[path.Clean(dir)] = append(groups[path.Clean(dir)], name)
	}
	return groups, nil
}

func (d *Driver) GetFileInfo(ctx context.Context, fileID string) (*domain.FileItem, error) {
	p := d.normalizePath(fileID)
	var resp fsGetResp
	if err := d.apiRequest(ctx, http.MethodPost, "/fs/get", fsGetReq{Path: p}, &resp, nil); err != nil {
		return nil, err
	}
	item := objToItem(p, resp.objResp)
	return &item, nil
}

func (d *Driver) ListFiles(ctx context.Context, parentID string) ([]domain.FileItem, error) {
	dir := d.normalizePath(parentID)
	var resp fsListResp
	if err := d.apiRequest(ctx, http.MethodPost, "/fs/list", fsListReq{
		pageReq: pageReq{Page: 1, PerPage: 0},
		Path:    dir,
		Refresh: d.add.RefreshList,
	}, &resp, nil); err != nil {
		return nil, err
	}
	items := make([]domain.FileItem, 0, len(resp.Content))
	for _, f := range resp.Content {
		items = append(items, fileToItem(dir, f))
	}
	return items, nil
}

func (d *Driver) ResolveDownload(ctx context.Context, req driver.DownloadRequest) (*domain.DownloadInfo, error) {
	p := d.normalizePath(req.FileID)
	headers := map[string]string{}
	if d.add.PassUA {
		headers["User-Agent"] = d.downloadUA(req.UA)
	}
	var resp fsGetResp
	if err := d.apiRequest(ctx, http.MethodPost, "/fs/get", fsGetReq{
		Path: p,
	}, &resp, headers); err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(resp.RawURL)
	if raw == "" {
		return nil, domain.Errorf(domain.CodeDriverError, "OpenList 未返回下载地址，该文件可能不支持直链")
	}
	mode := domain.DownloadProxy
	if strings.EqualFold(strings.TrimSpace(d.add.DownloadMode), "redirect") {
		mode = domain.DownloadRedirect
	}
	info := &domain.DownloadInfo{
		URL:        raw,
		Mode:       mode,
		Expiration: downloadURLExpiration,
		Size:       resp.Size,
		FileName:   resp.Name,
	}
	if mode == domain.DownloadProxy {
		info.Headers = http.Header{"User-Agent": []string{d.downloadUA(req.UA)}}
		info.ForceProxy = true
	}
	return info, nil
}

func (d *Driver) downloadUA(raw string) string {
	ua := strings.TrimSpace(raw)
	if ua == "" {
		return httpx.DefaultUserAgent
	}
	return ua
}

// apiRequest 发请求并解析 OpenList 通用包装；401/403 且有账号密码时自动重登后重试一次。
func (d *Driver) apiRequest(ctx context.Context, method, apiPath string, body, out any, headers map[string]string) error {
	if err := d.waitOperationDelay(ctx); err != nil {
		return err
	}
	err := d.rawRequest(ctx, method, apiPath, body, out, headers)
	if ae, ok := domain.AsAppError(err); ok && ae.Code == domain.CodeAuthExpired {
		if strings.TrimSpace(d.add.Username) == "" {
			return err
		}
		if lerr := d.login(ctx); lerr != nil {
			return lerr
		}
		if err := d.waitOperationDelay(ctx); err != nil {
			return err
		}
		return d.rawRequest(ctx, method, apiPath, body, out, headers)
	}
	return err
}

func (d *Driver) rawRequest(ctx context.Context, method, apiPath string, body, out any, headers map[string]string) error {
	u := strings.TrimRight(strings.TrimSpace(d.add.Address), "/") + "/api" + apiPath
	req, err := httpx.NewJSONRequest(ctx, method, u, nil, body)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	h := map[string]string{
		"Authorization": d.currentToken(),
		"User-Agent":    httpx.DefaultUserAgent,
	}
	for k, v := range headers {
		h[k] = v
	}
	httpx.SetHeaders(req, h)

	resp, data, err := httpx.Execute(d.client, req, httpx.DefaultReadLimit)
	if err != nil {
		return domain.Wrap(domain.CodeDriverError, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return domain.Errf(domain.CodeAuthExpired)
	}
	if resp.StatusCode != http.StatusOK {
		return domain.Errorf(domain.CodeDriverError, "OpenList HTTP %d: %s", resp.StatusCode, httpx.Truncate(data, 300))
	}
	var env respEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return domain.Wrap(domain.CodeDriverError, err)
	}
	if env.Code != 200 {
		if env.Code == 401 || env.Code == 403 {
			return domain.Errf(domain.CodeAuthExpired)
		}
		return mapAPIError(env.Code, env.Message)
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return domain.Wrap(domain.CodeDriverError, err)
		}
	}
	return nil
}

func mapAPIError(code int, msg string) error {
	switch code {
	case 404:
		return domain.Errf(domain.CodeNotFound)
	case 429:
		return domain.Errorf(domain.CodeRateLimited, "OpenList 限流：%s", msg)
	default:
		return domain.Errorf(domain.CodeDriverError, "OpenList 请求失败(%d)：%s", code, msg)
	}
}

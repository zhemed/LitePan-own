package openlist

import (
	"encoding/json"
	"path"
	"strings"
	"time"

	"litepan/internal/domain"
)

// guestRole 对应 OpenList 的 GUEST（游客模式）；挂载需站点开启“允许挂载”。
const guestRole = 1

const defaultOperationDelayMS = 300

// pageReq 与 OpenList 的分页参数一致；PerPage=0 表示一次返回全部。
type pageReq struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

type fsListReq struct {
	pageReq
	Path    string `json:"path"`
	Refresh bool   `json:"refresh"`
}

type objResp struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	IsDir    bool      `json:"is_dir"`
	Modified time.Time `json:"modified"`
	Created  time.Time `json:"created"`
	Sign     string    `json:"sign"`
	Thumb    string    `json:"thumb"`
	Type     int       `json:"type"`
	HashInfo string    `json:"hashinfo"`
}

type fsListResp struct {
	Content  []objResp `json:"content"`
	Total    int64     `json:"total"`
	Readme   string    `json:"readme"`
	Write    bool      `json:"write"`
	Provider string    `json:"provider"`
}

type fsGetReq struct {
	Path string `json:"path"`
}

type fsGetResp struct {
	objResp
	RawURL   string `json:"raw_url"`
	Provider string `json:"provider"`
}

type mkdirReq struct {
	Path string `json:"path"`
}

type renameReq struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type removeReq struct {
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
}

type dirOpReq struct {
	SrcDir string   `json:"src_dir"`
	DstDir string   `json:"dst_dir"`
	Names  []string `json:"names"`
}

type meResp struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Role     int    `json:"role"`
	Disabled bool   `json:"disabled"`
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResp struct {
	Token string `json:"token"`
}

type publicSettingsResp struct {
	AllowMounted string `json:"allow_mounted"`
}

// respEnvelope 是 OpenList 通用响应包装。
type respEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func fileToItem(dir string, f objResp) domain.FileItem {
	return objToItem(joinPath(dir, f.Name), f)
}

// objToItem 用给定的绝对路径作为文件 ID 构造 FileItem。
func objToItem(id string, f objResp) domain.FileItem {
	item := domain.FileItem{
		ID:      id,
		Name:    f.Name,
		Size:    f.Size,
		IsDir:   f.IsDir,
		ModTime: f.Modified,
		Thumb:   f.Thumb,
		IDKind:  domain.IDPath,
	}
	if h := parseHashInfo(f.HashInfo); len(h) > 0 {
		item.Hash = h
	}
	return item
}

// parseHashInfo 解析 OpenList 的 hashinfo（形如 {"md5":"...","sha1":"..."}）。
func parseHashInfo(info string) map[domain.HashType]string {
	info = strings.TrimSpace(info)
	if info == "" || !strings.HasPrefix(info, "{") {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(info), &m); err != nil {
		return nil
	}
	var out map[domain.HashType]string
	if v := strings.TrimSpace(m["md5"]); v != "" {
		out = make(map[domain.HashType]string, 2)
		out[domain.HashMD5] = v
	}
	if v := strings.TrimSpace(m["sha1"]); v != "" {
		if out == nil {
			out = make(map[domain.HashType]string, 1)
		}
		out[domain.HashSHA1] = v
	}
	return out
}

// rootPath 返回配置的根目录路径，默认 "/"。
func (d *Driver) rootPath() string {
	r := strings.TrimSpace(d.add.RootPath)
	if r == "" {
		return "/"
	}
	return "/" + strings.Trim(r, "/")
}

// normalizePath 把文件 ID（即 OpenList 绝对路径）规范为带前导斜杠的形式。
func (d *Driver) normalizePath(id string) string {
	p := strings.TrimSpace(id)
	if p == "" || p == "/" || p == "0" || strings.EqualFold(p, "root") {
		return d.rootPath()
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

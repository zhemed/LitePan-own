package upload

import (
	"context"
	"path"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/file"
)

func ensureUploadTargetDir(ctx context.Context, files *file.Service, accountID int64, rootID, relDir string) (string, error) {
	if files == nil {
		return "", domain.Errorf(domain.CodeInternal, "文件服务未就绪")
	}
	relDir = strings.Trim(relDir, "/")
	if relDir == "" {
		return rootID, nil
	}
	cur := rootID
	for _, part := range strings.Split(relDir, "/") {
		if part == "" {
			continue
		}
		items, err := files.List(ctx, accountID, cur, false)
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
			created, err := files.CreateFolder(ctx, accountID, cur, part)
			if err != nil {
				return "", err
			}
			next = created.ID
		}
		cur = next
	}
	return cur, nil
}

func joinUploadDisplayPath(base, relDir string) string {
	base = "/" + strings.Trim(strings.TrimSpace(base), "/")
	if base == "/" {
		base = ""
	}
	relDir = strings.Trim(strings.TrimSpace(relDir), "/")
	if relDir == "" {
		if base == "" {
			return "/"
		}
		return base
	}
	joined := path.Join(base, relDir)
	if joined == "." || joined == "" {
		return "/"
	}
	if !strings.HasPrefix(joined, "/") {
		return "/" + joined
	}
	return joined
}

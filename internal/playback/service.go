package playback

import (
	"context"
	"net/http"

	"litepan/internal/cache"
	"litepan/internal/core/driverexec"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

type Service struct {
	exec        *driverexec.Executor
	cache       *cache.Service
	clientHTTP1 *http.Client
	clientH2    *http.Client
	rangeLimits accountRangeLimiter
}

func NewService(exec *driverexec.Executor, c *cache.Service) *Service {
	return &Service{
		exec:        exec,
		cache:       c,
		clientHTTP1: &http.Client{Transport: newUpstreamTransport(false), CheckRedirect: stripRedirectReferer},
		clientH2:    &http.Client{Transport: newUpstreamTransport(true), CheckRedirect: stripRedirectReferer},
	}
}

func stripRedirectReferer(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	prev := via[len(via)-1]
	if prev.URL.Host != req.URL.Host || prev.URL.Scheme != req.URL.Scheme {
		req.Header.Del("Referer")
	}
	return nil
}

type Request struct {
	AccountID int64
	FileID    string
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request, req Request, intent Intent) error {
	if err := s.exec.Check(r.Context(), req.AccountID); err != nil {
		return err
	}
	ua := r.UserAgent()
	res, err := s.Resolve(r.Context(), req.AccountID, req.FileID, ua, false)
	if err != nil {
		return err
	}
	if res.File.IsDir {
		return domain.Errorf(domain.CodeValidation, "不能下载目录")
	}
	action := PickAction(res.Mode, res.Link, intent)
	if action == ActionRedirect {
		writeRedirect(w, r, res, intent)
		return nil
	}
	name := intent.FileName
	if name == "" {
		name = res.File.Name
	}
	return s.serveStream(w, r, req, res, name, ua, intent)
}

func (s *Service) Resolve(ctx context.Context, accountID int64, fileID, ua string, refresh bool) (Resolved, error) {
	if s.cache == nil {
		return s.resolveFresh(ctx, accountID, fileID, ua)
	}

	key := cache.DownloadURLKey(accountID, fileID, ua)
	if refresh {
		s.cache.InvalidateKey(key)
	} else if res, ok := cache.GetAs[Resolved](s.cache, key); ok {
		return res, nil
	}

	res, err := cache.CoalesceAs[Resolved](ctx, s.cache, key, func(callCtx context.Context) (Resolved, error) {
		if !refresh {
			if cached, ok := cache.GetAs[Resolved](s.cache, key); ok {
				return cached, nil
			}
		}
		fresh, err := s.resolveFresh(callCtx, accountID, fileID, ua)
		if err != nil {
			return Resolved{}, err
		}
		ttl := fresh.Link.Expiration
		if ttl <= 0 {
			ttl = defaultLinkTTL
		}
		cache.SetAs(s.cache, key, fresh, ttl)
		return fresh, nil
	})
	if err != nil {
		return Resolved{}, err
	}
	return res, nil
}

func (s *Service) resolveFresh(ctx context.Context, accountID int64, fileID, ua string) (Resolved, error) {
	var res Resolved
	err := s.exec.Run(ctx, accountID, func(drv driver.Driver) error {
		file := domain.FileItem{ID: fileID}
		dl, err := driverexec.Require[driver.Downloader](drv)
		if err != nil {
			return err
		}
		link, err := dl.ResolveDownload(ctx, driver.DownloadRequest{FileID: fileID, UA: ua})
		if err != nil {
			return err
		}
		if link.URL == "" && link.LocalPath == "" && !link.ForceProxy {
			return domain.Errorf(domain.CodeDriverError, "驱动未返回下载地址")
		}
		if file.Size <= 0 && link.Size > 0 {
			file.Size = link.Size
		}
		if file.Name == "" && link.FileName != "" {
			file.Name = link.FileName
		}
		if file.Name == "" || file.Size <= 0 {
			if info, ok := drv.(driver.InfoGetter); ok {
				got, err := info.GetFileInfo(ctx, fileID)
				if err != nil {
					return err
				}
				file = *got
				if file.Size <= 0 && link.Size > 0 {
					file.Size = link.Size
				}
				if file.Name == "" && link.FileName != "" {
					file.Name = link.FileName
				}
			}
		}
		mode := link.Mode
		if mode == domain.DownloadRedirect && link.ForceProxy {
			mode = domain.DownloadProxy
		}
		res = Resolved{File: file, Link: *link, Mode: mode}
		return nil
	})
	return res, err
}

func (s *Service) InvalidateAccount(accountID int64) {
	if s.cache != nil {
		s.cache.InvalidateAccountType(accountID, cache.TypeDownloadURL)
	}
}

func (s *Service) InvalidateAll() {
	if s.cache != nil {
		s.cache.InvalidatePrefix(string(cache.TypeDownloadURL) + ":")
	}
}

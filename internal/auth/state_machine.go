package auth

import (
	"context"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/eventbus"
)

func (s *Service) loadState(ctx context.Context, accountID int64) (*domain.AuthState, error) {
	st, err := s.authStates.Get(ctx, accountID)
	if err == nil {
		return st, nil
	}
	if ae, ok := domain.AsAppError(err); ok && ae.Code == domain.CodeNotFound {
		return &domain.AuthState{AccountID: accountID, Status: domain.AuthActive}, nil
	}
	return nil, err
}

func (s *Service) markSuccess(ctx context.Context, accountID int64, st *domain.AuthState) {
	wasBad := st.Status != domain.AuthActive
	st.Status = domain.AuthActive
	st.ActiveAttempts = 0
	st.PassiveAttempts = 0
	st.LastError = ""
	st.LastFailureKind = ""
	st.NextRetryAt = time.Time{}
	st.LastRefreshAt = s.now()
	if err := s.authStates.Upsert(ctx, st); err != nil {
		s.log.Warn("auth mark success", "account", accountID, "err", err)
		return
	}
	if wasBad && s.bus != nil {
		s.bus.Publish(ctx, eventbus.AccountAuthRecovered{AccountID: accountID})
	}
	s.wake()
}

// RecoverAccount 在用户手动更新凭证后恢复认证可用态（清除 failed/cooldown 并发布恢复事件）。
func (s *Service) RecoverAccount(ctx context.Context, accountID int64) {
	st, err := s.loadState(ctx, accountID)
	if err != nil {
		return
	}
	s.markSuccess(ctx, accountID, st)
}

func (s *Service) handleFailure(ctx context.Context, accountID int64, st *domain.AuthState, outcome driver.RefreshOutcome, caller driver.RefreshCaller, cause error) {
	now := s.now()
	kind := classifyFailureKind(outcome, cause)
	if outcome == driver.RefreshFatal {
		msg := "认证令牌已失效，需要重新授权"
		if cause != nil {
			msg = cause.Error()
		}
		s.toTokenExpired(ctx, accountID, st, msg)
		return
	}
	countFailure := kind != domain.AuthFailureNetwork
	if caller == driver.CallerActive {
		if countFailure {
			st.ActiveAttempts++
		}
		if countFailure && st.ActiveAttempts >= activeFailedThreshold {
			msg := "账号认证刷新连续失败，已暂停相关后台任务"
			if cause != nil {
				msg = cause.Error()
			}
			s.toFailed(ctx, accountID, st, msg)
			return
		}
		st.Status = domain.AuthCooldown
		st.NextRetryAt = now.Add(SteppedCooldown(max(st.ActiveAttempts, 1)))
		if cause != nil {
			st.LastError = cause.Error()
		} else {
			st.LastError = "主动刷新失败"
		}
	} else {
		if countFailure {
			st.PassiveAttempts++
		}
		if countFailure && st.PassiveAttempts >= passiveFailedThreshold {
			msg := "账号认证连续失败，已暂停相关后台任务"
			if cause != nil {
				msg = cause.Error()
			}
			s.toFailed(ctx, accountID, st, msg)
			return
		}
		st.Status = domain.AuthCooldown
		st.NextRetryAt = now.Add(passiveCooldown)
		if cause != nil {
			st.LastError = cause.Error()
		} else {
			st.LastError = "被动刷新失败"
		}
	}
	st.LastFailureKind = kind
	if err := s.authStates.Upsert(ctx, st); err != nil {
		s.log.Warn("auth handle failure", "account", accountID, "err", err)
	}
	s.wake()
}

func (s *Service) toTokenExpired(ctx context.Context, accountID int64, st *domain.AuthState, msg string) {
	st.Status = domain.AuthTokenExpired
	st.LastError = msg
	st.LastFailureKind = domain.AuthFailureAuth
	st.NextRetryAt = s.now().Add(failedRetryCooldown)
	if err := s.authStates.Upsert(ctx, st); err != nil {
		s.log.Warn("auth token expired", "account", accountID, "err", err)
	}
	s.publishFailed(ctx, accountID, msg, true)
	s.wake()
}

func (s *Service) toFailed(ctx context.Context, accountID int64, st *domain.AuthState, msg string) {
	st.Status = domain.AuthFailed
	st.LastError = msg
	st.LastFailureKind = domain.AuthFailureAuth
	st.NextRetryAt = s.now().Add(failedRetryCooldown)
	if err := s.authStates.Upsert(ctx, st); err != nil {
		s.log.Warn("auth failed", "account", accountID, "err", err)
	}
	s.publishFailed(ctx, accountID, msg, false)
	s.wake()
}

func (s *Service) publishFailed(ctx context.Context, accountID int64, reason string, fatal bool) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(ctx, eventbus.AccountAuthFailed{
		AccountID: accountID,
		Reason:    reason,
		Fatal:     fatal,
	})
}

func authBlocked(st *domain.AuthState, now time.Time) error {
	switch st.Status {
	case domain.AuthTokenExpired:
		return domain.Errorf(domain.CodeAuthExpired, "账号认证令牌已失效，需要重新授权")
	case domain.AuthFailed:
		return domain.Errorf(domain.CodeAuthExpired, "账号认证已失效，请稍后重试或重新授权")
	case domain.AuthCooldown:
		if !st.NextRetryAt.IsZero() && now.Before(st.NextRetryAt) {
			remaining := int(st.NextRetryAt.Sub(now).Seconds())
			if remaining < 1 {
				remaining = 1
			}
			return domain.Errorf(domain.CodeAuthExpired, "账号认证处于冷却期，剩余 %d 秒后允许再次尝试", remaining)
		}
	}
	return nil
}

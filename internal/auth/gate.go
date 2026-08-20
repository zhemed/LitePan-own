package auth

import (
	"context"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// Gate 是被动刷新闸门：请求前检查状态，401 时触发刷新。
type Gate struct {
	svc *Service
}

var _ GateChecker = (*Gate)(nil)

// Check 请求前闸门；failed/token_expired 阻断，冷却未到期阻断，冷却到期尝试被动刷新。
func (g *Gate) Check(ctx context.Context, accountID int64) error {
	if g == nil || g.svc == nil {
		return nil
	}
	st, err := g.svc.loadState(ctx, accountID)
	if err != nil {
		return err
	}
	now := g.svc.now()
	switch st.Status {
	case domain.AuthFailed:
		return authBlocked(st, now)
	case domain.AuthTokenExpired:
		if err := g.HandlePassiveError(ctx, accountID); err != nil {
			return authBlocked(st, now)
		}
		return nil
	case domain.AuthCooldown:
		if passiveBypassesCooldown(st) {
			return g.HandlePassiveError(ctx, accountID)
		}
		if !st.NextRetryAt.IsZero() && now.Before(st.NextRetryAt) {
			return authBlocked(st, now)
		}
		return g.HandlePassiveError(ctx, accountID)
	default:
		return nil
	}
}

// HandlePassiveError 被动刷新入口（请求遇到认证错误时调用）。
func (g *Gate) HandlePassiveError(ctx context.Context, accountID int64) error {
	if g == nil || g.svc == nil {
		return nil
	}
	unlock := g.svc.locks.Lock(accountID)
	defer unlock()

	st, err := g.svc.loadState(ctx, accountID)
	if err != nil {
		return err
	}
	now := g.svc.now()
	if st.Status == domain.AuthActive && !st.LastRefreshAt.IsZero() {
		if now.Sub(st.LastRefreshAt) < passiveReuseWindow {
			return nil
		}
	}
	if st.Status == domain.AuthCooldown && !st.NextRetryAt.IsZero() && now.Before(st.NextRetryAt) {
		if !passiveBypassesCooldown(st) {
			return authBlocked(st, now)
		}
	}
	if st.Status == domain.AuthFailed || st.Status == domain.AuthTokenExpired {
		return authBlocked(st, now)
	}

	outcome, rerr := g.svc.refreshUnlocked(ctx, accountID, driver.CallerPassive)
	if outcome == driver.RefreshSuccess {
		return nil
	}
	if rerr != nil {
		return rerr
	}
	return domain.Errf(domain.CodeAuthExpired)
}

package auth

import (
	"litepan/internal/domain"
	"litepan/internal/driver"
)

func classifyFailureKind(outcome driver.RefreshOutcome, cause error) domain.AuthFailureKind {
	if outcome == driver.RefreshFatal {
		return domain.AuthFailureAuth
	}
	if domain.IsNetworkError(cause) {
		return domain.AuthFailureNetwork
	}
	return domain.AuthFailureAuth
}

func passiveBypassesCooldown(st *domain.AuthState) bool {
	return st != nil && st.LastFailureKind == domain.AuthFailureNetwork
}

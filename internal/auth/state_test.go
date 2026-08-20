package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/eventbus"
)

type fakeAccountRepo struct {
	accounts map[int64]*domain.Account
}

func (r *fakeAccountRepo) Create(context.Context, *domain.Account) (int64, error) { return 0, nil }
func (r *fakeAccountRepo) Update(context.Context, *domain.Account) error          { return nil }
func (r *fakeAccountRepo) Delete(context.Context, int64) error                    { return nil }
func (r *fakeAccountRepo) SetDefault(context.Context, int64) error                { return nil }
func (r *fakeAccountRepo) NameTaken(context.Context, string, int64) (bool, error) { return false, nil }
func (r *fakeAccountRepo) Get(_ context.Context, id int64) (*domain.Account, error) {
	if a, ok := r.accounts[id]; ok {
		cp := *a
		return &cp, nil
	}
	return nil, domain.Errf(domain.CodeNotFound)
}
func (r *fakeAccountRepo) List(context.Context) ([]*domain.Account, error) {
	out := make([]*domain.Account, 0, len(r.accounts))
	for _, a := range r.accounts {
		cp := *a
		out = append(out, &cp)
	}
	return out, nil
}

type fakeAuthRepo struct {
	mu     sync.Mutex
	states map[int64]*domain.AuthState
}

func (r *fakeAuthRepo) Get(_ context.Context, accountID int64) (*domain.AuthState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if st, ok := r.states[accountID]; ok {
		cp := *st
		return &cp, nil
	}
	return nil, domain.Errf(domain.CodeNotFound)
}
func (r *fakeAuthRepo) Upsert(_ context.Context, st *domain.AuthState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *st
	r.states[st.AccountID] = &cp
	return nil
}
func (r *fakeAuthRepo) Delete(_ context.Context, accountID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, accountID)
	return nil
}

type fakeProvider struct {
	drv driver.Driver
}

func (p fakeProvider) Get(context.Context, int64) (driver.Driver, error) { return p.drv, nil }

type refreshDriver struct {
	outcome driver.RefreshOutcome
	err     error
	calls   int
}

func (d *refreshDriver) Config() driver.Config {
	return driver.Config{
		Name:           "refresh_test",
		AuthType:       driver.AuthToken,
		TokenLifetime:  30 * 24 * time.Hour,
		RefreshAdvance: 10 * time.Hour,
	}
}
func (d *refreshDriver) GetAddition() any           { return &struct{}{} }
func (d *refreshDriver) Init(context.Context) error { return nil }
func (d *refreshDriver) Drop(context.Context) error { return nil }
func (d *refreshDriver) Ping(context.Context) error { return nil }
func (d *refreshDriver) ListFiles(context.Context, string) ([]domain.FileItem, error) {
	return nil, nil
}
func (d *refreshDriver) RefreshAuth(context.Context, driver.RefreshCaller) (driver.RefreshOutcome, error) {
	d.calls++
	if d.err != nil {
		return driver.RefreshRetryable, d.err
	}
	return d.outcome, nil
}

func init() {
	driver.Register(func() driver.Driver { return &refreshDriver{outcome: driver.RefreshSuccess} })
}

func newTestService(now time.Time, outcome driver.RefreshOutcome) (*Service, *fakeAuthRepo, *refreshDriver, *eventbus.Bus) {
	authRepo := &fakeAuthRepo{states: map[int64]*domain.AuthState{}}
	drv := &refreshDriver{outcome: outcome}
	bus := eventbus.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc := NewService(Options{
		Accounts:   &fakeAccountRepo{accounts: map[int64]*domain.Account{1: {ID: 1, DriverType: "refresh_test", IsActive: true}}},
		AuthStates: authRepo,
		Drivers:    fakeProvider{drv: drv},
		Bus:        bus,
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:        func() time.Time { return now },
	})
	svc.Register(1)
	return svc, authRepo, drv, bus
}

func TestSchedulerRefreshesExpiringTokenAccount(t *testing.T) {
	now := time.Now()
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())

	repo.states[1] = &domain.AuthState{
		AccountID:    1,
		Status:       domain.AuthActive,
		TokenExpires: now.Add(10 * time.Minute),
	}
	scheduler := NewScheduler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	scheduler.firstExec = false
	scheduler.executeCheck(context.Background())
	if drv.calls != 1 {
		t.Fatalf("expected active refresh, got calls=%d", drv.calls)
	}
}

func TestActiveRefreshRetryableEntersSteppedCooldown(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, _, bus := newTestService(now, driver.RefreshRetryable)
	defer bus.Close(context.Background())

	repo.states[1] = &domain.AuthState{AccountID: 1, Status: domain.AuthActive}
	outcome, err := svc.Refresh(context.Background(), 1, driver.CallerActive)
	if err == nil || outcome != driver.RefreshRetryable {
		t.Fatalf("expected retryable error, got outcome=%s err=%v", outcome, err)
	}
	st, _ := repo.Get(context.Background(), 1)
	if st.Status != domain.AuthCooldown || st.ActiveAttempts != 1 {
		t.Fatalf("unexpected state: %+v", st)
	}
	if !st.NextRetryAt.Equal(now.Add(60 * time.Second)) {
		t.Fatalf("next retry = %v", st.NextRetryAt)
	}
}

func TestActiveRefreshFifthFailureBecomesFailed(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, _, bus := newTestService(now, driver.RefreshRetryable)
	defer bus.Close(context.Background())

	repo.states[1] = &domain.AuthState{AccountID: 1, Status: domain.AuthCooldown, ActiveAttempts: 4}
	_, _ = svc.Refresh(context.Background(), 1, driver.CallerActive)
	st, _ := repo.Get(context.Background(), 1)
	if st.Status != domain.AuthFailed || st.ActiveAttempts != 5 {
		t.Fatalf("unexpected state: %+v", st)
	}
}

func TestPassiveGateReusesRecentSuccessfulRefresh(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())

	repo.states[1] = &domain.AuthState{AccountID: 1, Status: domain.AuthActive, LastRefreshAt: now.Add(-5 * time.Second)}
	if err := svc.Gate().HandlePassiveError(context.Background(), 1); err != nil {
		t.Fatalf("passive gate: %v", err)
	}
	if drv.calls != 0 {
		t.Fatalf("recent success should be reused, calls=%d", drv.calls)
	}
}

func TestCheckRefreshesWhenCooldownExpired(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())

	repo.states[1] = &domain.AuthState{AccountID: 1, Status: domain.AuthCooldown, NextRetryAt: now.Add(-time.Second)}
	if err := svc.Gate().Check(context.Background(), 1); err != nil {
		t.Fatalf("check should refresh expired cooldown: %v", err)
	}
	if drv.calls != 1 {
		t.Fatalf("expected one refresh, got %d", drv.calls)
	}
	st, _ := repo.Get(context.Background(), 1)
	if st.Status != domain.AuthActive || !st.LastRefreshAt.Equal(now) {
		t.Fatalf("unexpected state: %+v", st)
	}
}

func TestCheckBlocksDuringCooldown(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())

	repo.states[1] = &domain.AuthState{
		AccountID:       1,
		Status:          domain.AuthCooldown,
		NextRetryAt:     now.Add(time.Minute),
		LastFailureKind: domain.AuthFailureAuth,
	}
	err := svc.Gate().Check(context.Background(), 1)
	if ae, ok := domain.AsAppError(err); !ok || ae.Code != domain.CodeAuthExpired {
		t.Fatalf("expected auth expired, got %v", err)
	}
	if drv.calls != 0 {
		t.Fatalf("auth cooldown should block without refresh, calls=%d", drv.calls)
	}
}

func TestCheckBypassesNetworkCooldown(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())

	repo.states[1] = &domain.AuthState{
		AccountID:       1,
		Status:          domain.AuthCooldown,
		NextRetryAt:     now.Add(30 * time.Minute),
		LastFailureKind: domain.AuthFailureNetwork,
		LastError:       "dial tcp: i/o timeout",
	}
	if err := svc.Gate().Check(context.Background(), 1); err != nil {
		t.Fatalf("network cooldown should allow passive refresh: %v", err)
	}
	if drv.calls != 1 {
		t.Fatalf("expected one refresh, got %d", drv.calls)
	}
	st, _ := repo.Get(context.Background(), 1)
	if st.Status != domain.AuthActive {
		t.Fatalf("expected active after refresh, got %+v", st)
	}
}

func TestNetworkFailureDoesNotIncrementAttempts(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())
	drv.err = errors.New("dial tcp: i/o timeout")

	repo.states[1] = &domain.AuthState{AccountID: 1, Status: domain.AuthActive}
	for i := 0; i < 8; i++ {
		outcome, err := svc.Refresh(context.Background(), 1, driver.CallerActive)
		if err == nil || outcome != driver.RefreshRetryable {
			t.Fatalf("iteration %d: expected retryable network error, got outcome=%s err=%v", i, outcome, err)
		}
	}
	st, _ := repo.Get(context.Background(), 1)
	if st.Status != domain.AuthCooldown {
		t.Fatalf("expected cooldown, got %+v", st)
	}
	if st.ActiveAttempts != 0 {
		t.Fatalf("network failures should not increment attempts, got %d", st.ActiveAttempts)
	}
	if st.LastFailureKind != domain.AuthFailureNetwork {
		t.Fatalf("expected network failure kind, got %q", st.LastFailureKind)
	}
}

func TestNetworkOutageThenUserAccessRecovers(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())
	drv.err = errors.New("connection refused")

	_, _ = svc.Refresh(context.Background(), 1, driver.CallerActive)
	st, _ := repo.Get(context.Background(), 1)
	if st.LastFailureKind != domain.AuthFailureNetwork {
		t.Fatalf("expected network kind after outage refresh, got %+v", st)
	}

	drv.err = nil
	if err := svc.Gate().Check(context.Background(), 1); err != nil {
		t.Fatalf("user access should bypass network cooldown: %v", err)
	}
	st, _ = repo.Get(context.Background(), 1)
	if st.Status != domain.AuthActive || st.LastFailureKind != "" {
		t.Fatalf("expected recovered active state, got %+v", st)
	}
}

func TestPassiveBypassUpdatesKindOnAuthFailure(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshRetryable)
	defer bus.Close(context.Background())
	drv.err = domain.Errorf(domain.CodeAuthExpired, "refresh token is invalid")

	repo.states[1] = &domain.AuthState{
		AccountID:       1,
		Status:          domain.AuthCooldown,
		NextRetryAt:     now.Add(30 * time.Minute),
		LastFailureKind: domain.AuthFailureNetwork,
	}
	err := svc.Gate().Check(context.Background(), 1)
	if err == nil {
		t.Fatal("expected auth error after bypass refresh")
	}
	st, _ := repo.Get(context.Background(), 1)
	if st.LastFailureKind != domain.AuthFailureAuth {
		t.Fatalf("expected auth failure kind after non-network refresh fail, got %+v", st)
	}
	if drv.calls != 1 {
		t.Fatalf("expected one bypass refresh attempt, got %d", drv.calls)
	}
}

func TestFatalRefreshPublishesFailureEvent(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, _, bus := newTestService(now, driver.RefreshFatal)
	defer bus.Close(context.Background())

	got := make(chan eventbus.AccountAuthFailed, 1)
	eventbus.Subscribe(bus, func(_ context.Context, e eventbus.AccountAuthFailed) { got <- e })
	repo.states[1] = &domain.AuthState{AccountID: 1, Status: domain.AuthActive}
	_, _ = svc.Refresh(context.Background(), 1, driver.CallerPassive)

	select {
	case e := <-got:
		if !e.Fatal || e.AccountID != 1 {
			t.Fatalf("unexpected event: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("expected auth failure event")
	}
}

func TestRefreshTreatsDriverErrorAsRetryable(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	svc, repo, drv, bus := newTestService(now, driver.RefreshSuccess)
	defer bus.Close(context.Background())
	drv.err = errors.New("temporary network error")

	repo.states[1] = &domain.AuthState{AccountID: 1, Status: domain.AuthActive}
	outcome, err := svc.Refresh(context.Background(), 1, driver.CallerActive)
	if err == nil || outcome != driver.RefreshRetryable {
		t.Fatalf("expected retryable error, got outcome=%s err=%v", outcome, err)
	}
}

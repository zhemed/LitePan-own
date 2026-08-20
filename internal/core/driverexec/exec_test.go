package driverexec

import (
	"context"
	"testing"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

type stubDriver struct{}

func (stubDriver) Config() driver.Config      { return driver.Config{Name: "stub"} }
func (stubDriver) GetAddition() any           { return struct{}{} }
func (stubDriver) Init(context.Context) error { return nil }
func (stubDriver) Drop(context.Context) error { return nil }
func (stubDriver) Ping(context.Context) error { return nil }
func (stubDriver) ListFiles(context.Context, string) ([]domain.FileItem, error) {
	return nil, nil
}

type stubProvider struct{ d driver.Driver }

func (p stubProvider) Get(context.Context, int64) (driver.Driver, error) { return p.d, nil }

type gateProbe struct {
	checks   int
	passives int
}

func (g *gateProbe) Check(context.Context, int64) error {
	g.checks++
	return nil
}
func (g *gateProbe) HandlePassiveError(context.Context, int64) error {
	g.passives++
	return nil
}

type flakyDriver struct{ calls int }

func (d *flakyDriver) Config() driver.Config      { return driver.Config{Name: "flaky"} }
func (d *flakyDriver) GetAddition() any           { return &struct{}{} }
func (d *flakyDriver) Init(context.Context) error { return nil }
func (d *flakyDriver) Drop(context.Context) error { return nil }
func (d *flakyDriver) Ping(context.Context) error { return nil }
func (d *flakyDriver) ListFiles(context.Context, string) ([]domain.FileItem, error) {
	d.calls++
	if d.calls == 1 {
		return nil, domain.Errf(domain.CodeAuthExpired)
	}
	return []domain.FileItem{{ID: "1"}}, nil
}

func TestCheckAndRun(t *testing.T) {
	gate := &gateProbe{}
	exec := New(stubProvider{d: stubDriver{}}, gate)
	ctx := context.Background()

	if err := exec.Check(ctx, 1); err != nil {
		t.Fatalf("check: %v", err)
	}
	if gate.checks != 1 {
		t.Fatalf("checks=%d want 1", gate.checks)
	}

	called := false
	if err := exec.Run(ctx, 1, func(driver.Driver) error {
		called = true
		return nil
	}); err != nil || !called {
		t.Fatalf("run: err=%v called=%v", err, called)
	}
}

func TestRunPassiveRetry(t *testing.T) {
	gate := &gateProbe{}
	d := &flakyDriver{}
	exec := New(stubProvider{d: d}, gate)
	ctx := context.Background()

	var items []domain.FileItem
	err := exec.Run(ctx, 1, func(drv driver.Driver) error {
		got, err := drv.ListFiles(ctx, "0")
		if err != nil {
			return err
		}
		items = got
		return nil
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%v", items)
	}
	if d.calls != 2 {
		t.Fatalf("driver calls=%d want 2", d.calls)
	}
	if gate.passives != 1 {
		t.Fatalf("passives=%d want 1", gate.passives)
	}
}

func TestRequireCapability(t *testing.T) {
	if _, err := Require[driver.Deleter](stubDriver{}); err == nil {
		t.Fatal("stub should not implement Deleter")
	}
}

package driverexec

import (
	"context"

	"litepan/internal/auth"
	"litepan/internal/domain"
	"litepan/internal/driver"
)

// Executor 是文件域调用驱动的统一中轴：Gate 检查 → 被动刷新重试 → 解析驱动实例。
type Executor struct {
	drivers driver.Provider
	gate    auth.GateChecker
}

func New(drivers driver.Provider, gate auth.GateChecker) *Executor {
	return &Executor{drivers: drivers, gate: gate}
}

func (e *Executor) Check(ctx context.Context, accountID int64) error {
	if e == nil || e.gate == nil {
		return nil
	}
	return e.gate.Check(ctx, accountID)
}

// Run 在认证闸门与被动刷新保护下执行驱动调用。
func (e *Executor) Run(ctx context.Context, accountID int64, fn func(driver.Driver) error) error {
	if e == nil {
		return domain.Errorf(domain.CodeInternal, "驱动执行器未初始化")
	}
	return auth.WithRetry(ctx, e.gate, accountID, func() error {
		drv, err := e.drivers.Get(ctx, accountID)
		if err != nil {
			return err
		}
		err = fn(drv)
		if err != nil && domain.IsNetworkError(err) {
			e.resetTransport(ctx, accountID)
		}
		return err
	})
}

func (e *Executor) resetTransport(ctx context.Context, accountID int64) {
	if e == nil || e.drivers == nil {
		return
	}
	if tr, ok := e.drivers.(driver.TransportResetter); ok {
		tr.ResetTransport(ctx, accountID)
	}
}

// Require 探测驱动可选能力；未实现时返回 NOT_IMPLEMENT。
func Require[T any](drv driver.Driver) (T, error) {
	cap, ok := drv.(T)
	if !ok {
		var zero T
		return zero, domain.Errf(domain.CodeNotImplement)
	}
	return cap, nil
}

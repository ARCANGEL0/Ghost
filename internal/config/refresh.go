package config

import (
	"log/slog"
	"sync/atomic"
)

// dynamicRefreshRunning guards against concurrent Refresh_Cycles. Only one
// background refresh may run at a time; additional triggers are no-ops while a
// cycle is in flight.
var dynamicRefreshRunning atomic.Bool

func RefreshDynamicProvidersInBackground(autoUpdateDisabled bool, cfgs ...*Config) {
	if autoUpdateDisabled {
		slog.Debug("Provider auto-update disabled; skipping background model refresh")
		return
	}
	if !dynamicRefreshRunning.CompareAndSwap(false, true) {
		slog.Debug("Model refresh already running; skipping duplicate trigger")
		return
	}

	go func() {
		defer dynamicRefreshRunning.Store(false)
		refreshDynamicProvidersOnce(cfgs...)
	}()
}

// refreshDynamicProvidersOnce performs one Refresh_Cycle synchronously. It is
// the unit of work executed by the background goroutine and is also directly
// testable.
func refreshDynamicProvidersOnce(cfgs ...*Config) {
	fresh := fetchFreshDynamicProviders(cfgs...)
	if len(fresh) == 0 {
		// All remote sources failed or returned nothing usable — keep the
		// existing cache contents unchanged.
		slog.Warn("Model refresh produced no providers; keeping existing cache")
		return
	}

	cached, _ := loadDynamicProvidersFromCache()
	merged := mergeProviders(cached, fresh)
	if err := saveDynamicProvidersToCache(merged); err != nil {
		slog.Warn("Failed to persist refreshed model cache", "error", err)
		return
	}

	slog.Info("Refreshed dynamic provider model cache", "providers", len(fresh))
}

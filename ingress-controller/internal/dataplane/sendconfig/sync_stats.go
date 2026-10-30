package sendconfig

import "context"

// SyncStats collects the outcome of a DB-mode (deck) sync performed by UpdateStrategyDBMode, which is used both
// for DB-backed Kong Gateways and for Konnect. It is only filled when requested by putting it into the context
// passed to PerformUpdate using ContextWithSyncStats.
// Currently only the Konnect ConfigSynchronizer requests it to record its sync round metrics;
// KongClient does not request it when configuring Kong Gateways.
type SyncStats struct {
	// Attempted is set when UpdateStrategyDBMode started syncing the configuration.
	Attempted bool
	// CreateOps, UpdateOps and DeleteOps are the numbers of entities successfully created, updated and deleted during the sync.
	CreateOps, UpdateOps, DeleteOps int
}

type syncStatsKey struct{}

// ContextWithSyncStats returns a copy of ctx carrying s to be filled by UpdateStrategyDBMode.
func ContextWithSyncStats(ctx context.Context, s *SyncStats) context.Context {
	return context.WithValue(ctx, syncStatsKey{}, s)
}

// SyncStatsFromContext returns the SyncStats carried by ctx, if any.
func SyncStatsFromContext(ctx context.Context) (*SyncStats, bool) {
	s, ok := ctx.Value(syncStatsKey{}).(*SyncStats)
	return s, ok && s != nil
}

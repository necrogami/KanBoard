// Package jobkind holds the background job constants that both sides of
// the queue need: the service enqueues rows inside its mutation
// transaction and the jobs runner leases and executes them. It imports
// nothing, so neither side has to depend on the other for a string.
package jobkind

const (
	// RankRebalance rebuilds one column's fractional keys when they have
	// grown past order.MaxKeyLen (spec 4.4). Payload: {"column_id": "..."}.
	// Plan 7 registers the handler.
	RankRebalance = "rank.rebalance"

	// DefaultMaxAttempts is the attempt budget a job gets when whoever
	// enqueued it did not ask for a different one.
	DefaultMaxAttempts = 8
)

// Package service is the single mutation path. Every command, whatever
// adapter it came from, is validated, authorized, applied inside one
// transaction together with its events, outbox jobs and idempotency
// receipt, and only then published.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/necrogami/kanboard/internal/core/clock"
	"github.com/necrogami/kanboard/internal/core/commands"
	"github.com/necrogami/kanboard/internal/core/events"
	"github.com/necrogami/kanboard/internal/core/id"
	"github.com/necrogami/kanboard/internal/core/policy"
	"github.com/necrogami/kanboard/internal/server/bus"
	"github.com/necrogami/kanboard/internal/server/store"
	"github.com/necrogami/kanboard/internal/server/store/sqlitegen"
)

// Code is a stable machine-readable error code (spec 5.3).
type Code string

const (
	CodeNotFound   Code = "E_NOT_FOUND"
	CodeForbidden  Code = "E_FORBIDDEN"
	CodeConflict   Code = "E_CONFLICT"
	CodeValidation Code = "E_VALIDATION"
)

// Error is what adapters translate into problem+json or MCP errors.
type Error struct {
	Code           Code
	Msg            string
	CurrentVersion int64
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Msg }

func notFound(what string) error { return &Error{Code: CodeNotFound, Msg: what + " not found"} }
func forbidden() error           { return &Error{Code: CodeForbidden, Msg: "not allowed"} }
func conflict(msg string, cur int64) error {
	return &Error{Code: CodeConflict, Msg: msg, CurrentVersion: cur}
}
func validation(err error) error { return &Error{Code: CodeValidation, Msg: err.Error()} }

// OutboxHook runs inside the mutation transaction for every event, so
// job rows (webhook deliveries, later) commit atomically with the event.
type OutboxHook func(ctx context.Context, q store.Querier, ev events.Event) error

// Service executes commands.
type Service struct {
	st    *store.Store
	clock clock.Clock
	bus   *bus.Bus
	hooks []OutboxHook
	after []func()
	newID func() string
	locks sync.Map // workspace id -> *sync.Mutex
}

// Option configures a Service.
type Option func(*Service)

// WithClock injects a clock (tests use clock.Fake).
func WithClock(c clock.Clock) Option { return func(s *Service) { s.clock = c } }

// WithBus sets the bus that receives events after commit.
func WithBus(b *bus.Bus) Option { return func(s *Service) { s.bus = b } }

// WithOutboxHook adds a hook that runs inside the transaction per event.
func WithOutboxHook(h OutboxHook) Option { return func(s *Service) { s.hooks = append(s.hooks, h) } }

// WithAfterCommit adds a function called after every successful commit
// and publish; plan 3 registers the job runner's Wake here (spec 5.1).
func WithAfterCommit(f func()) Option { return func(s *Service) { s.after = append(s.after, f) } }

func (s *Service) workspaceLock(workspaceID string) *sync.Mutex {
	v, _ := s.locks.LoadOrStore(workspaceID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// WithIDs replaces the id generator (tests use deterministic ids).
func WithIDs(f func() string) Option { return func(s *Service) { s.newID = f } }

// New wires a Service over st.
func New(st *store.Store, opts ...Option) *Service {
	s := &Service{st: st, clock: clock.Real{}, newID: id.New}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Subscribe returns a bus subscription that carries only the events
// actor is allowed to read: workspace membership does not imply project
// membership, and an unfiltered workspace stream would hand a member the
// titles of cards in projects they have no role on. The Evicted signal
// and Close behave exactly as on a raw bus subscription.
//
// It returns nil when the service was built without a bus.
func (s *Service) Subscribe(actor policy.Actor) *bus.Sub {
	if s.bus == nil {
		return nil
	}
	return s.bus.SubscribeFunc(actor.WorkspaceID, func(ev events.Event) bool {
		return policy.Can(actor, policy.ProjectRead, policy.Resource{ProjectID: ev.ProjectID, BoardID: ev.BoardID})
	})
}

// Tx is the per-command transaction context handed to command bodies.
type Tx struct {
	ctx    context.Context
	Q      store.Querier
	Now    time.Time
	NowMs  int64
	s      *Service
	actor  policy.Actor
	meta   commands.Meta
	events []events.Event
}

// scope says which rows an event is about.
type scope struct {
	WorkspaceID, ProjectID, BoardID, CardID string
}

// emit queues an event to be appended (with seq) at the end of the
// transaction and published after commit.
func (t *Tx) emit(kind events.Kind, sc scope, payload any) error {
	ev, err := events.New(kind, payload)
	if err != nil {
		return err
	}
	ev.WorkspaceID, ev.ProjectID, ev.BoardID, ev.CardID = sc.WorkspaceID, sc.ProjectID, sc.BoardID, sc.CardID
	t.events = append(t.events, ev)
	return nil
}

func (t *Tx) can(act policy.Action, res policy.Resource) error {
	if !policy.Can(t.actor, act, res) {
		return forbidden()
	}
	return nil
}

func (t *Tx) appendEvent(ev *events.Event) error {
	seq, err := t.Q.NextSeq(t.ctx, ev.WorkspaceID)
	if err != nil {
		return fmt.Errorf("next seq: %w", err)
	}
	ev.ID = t.s.newID()
	ev.Seq = seq
	ev.OccurredAt = t.Now
	ev.ActorUserID = t.actor.UserID
	ev.ViaTokenID = t.meta.ViaTokenID
	switch {
	case t.actor.UserID == "":
		ev.ActorKind = events.ActorSystem
	case t.actor.Token != nil:
		ev.ActorKind = events.ActorAgent
	default:
		ev.ActorKind = events.ActorHuman
	}
	return t.Q.InsertEvent(t.ctx, sqlitegen.InsertEventParams{
		ID: ev.ID, WorkspaceID: ev.WorkspaceID,
		ProjectID: nullStr(ev.ProjectID), BoardID: nullStr(ev.BoardID), CardID: nullStr(ev.CardID),
		Seq: seq, ActorUserID: nullStr(ev.ActorUserID), ViaTokenID: nullStr(ev.ViaTokenID),
		ActorKind: string(ev.ActorKind), Kind: string(ev.Kind), Payload: ev.Payload, OccurredAt: t.NowMs,
	})
}

// run executes one command: receipt replay, body, events, hooks,
// receipt, commit, publish. out must be a pointer to the JSON-encodable
// result so a replay can be answered from the receipt. kind is a stable
// literal naming the command (e.g. "CreateProject"); a stored receipt
// whose kind differs from kind means the idempotency key was reused for
// a different command, which is E_CONFLICT rather than a replay.
func (s *Service) run(ctx context.Context, actor policy.Actor, meta commands.Meta, kind string, out any, body func(tx *Tx) error) error {
	// One mutation at a time per workspace, held through publish, so seq
	// order equals publish order. The database already serializes writes
	// (BEGIN IMMEDIATE on SQLite, the workspace row lock on Postgres);
	// this only extends that ordering to the bus.
	mu := s.workspaceLock(actor.WorkspaceID)
	mu.Lock()
	defer mu.Unlock()

	var pending []events.Event
	err := s.st.WithTx(ctx, func(q store.Querier) error {
		if meta.IdempotencyKey != "" {
			r, err := q.GetReceipt(ctx, sqlitegen.GetReceiptParams{IdempotencyKey: meta.IdempotencyKey, ActorID: actor.UserID})
			switch {
			case err == nil:
				if r.CommandKind != kind {
					return conflict("idempotency key reused for a different command", 0)
				}
				return json.Unmarshal([]byte(r.Result), out)
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
		}
		now := s.clock.Now()
		tx := &Tx{ctx: ctx, Q: q, Now: now, NowMs: clock.Millis(now), s: s, actor: actor, meta: meta}
		if err := body(tx); err != nil {
			return err
		}
		for i := range tx.events {
			if err := tx.appendEvent(&tx.events[i]); err != nil {
				return err
			}
			for _, h := range s.hooks {
				if err := h(ctx, q, tx.events[i]); err != nil {
					return err
				}
			}
		}
		if meta.IdempotencyKey != "" {
			b, err := json.Marshal(out)
			if err != nil {
				return err
			}
			if err := q.InsertReceipt(ctx, sqlitegen.InsertReceiptParams{WorkspaceID: nullStr(actor.WorkspaceID), IdempotencyKey: meta.IdempotencyKey, ActorID: actor.UserID, CommandKind: kind, Result: string(b), CreatedAt: tx.NowMs}); err != nil {
				return err
			}
		}
		pending = tx.events
		return nil
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			// A concurrent insert won: duplicate key, or a same-key retry
			// whose receipt was written by the other transaction. The
			// caller retries and is answered from the receipt.
			return conflict("already exists", 0)
		}
		return err
	}
	if s.bus != nil {
		for _, ev := range pending {
			s.bus.Publish(ev)
		}
	}
	for _, f := range s.after {
		f()
	}
	return nil
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func nullMs(t *time.Time) sql.NullInt64 {
	if t == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: clock.Millis(*t), Valid: true}
}

func msPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := clock.FromMillis(v.Int64)
	return &t
}

func strPtr(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

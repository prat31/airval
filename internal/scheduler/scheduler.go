// Package scheduler fires schedule triggers. It runs on every server replica but only the leader
// (elected through a NATS KV lease) polls for due triggers. Each fire is claimed with an
// optimistic update of next_fire_at, so a trigger fires exactly once even across a leader change.
//
// Missed-fire policy: a trigger that is overdue (server down, leader change) fires once, and its
// next fire time is computed from now — missed occurrences are not replayed.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/prat31/airval/internal/planner"
	"github.com/prat31/airval/internal/queue"
	"github.com/prat31/airval/internal/store"
)

// Parser accepts 5-field cron expressions and descriptors (@daily, @every 1h…).
var Parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Next returns the next fire time after `after` for a cron expression in a timezone (UTC when empty).
func Next(expr, tz string, after time.Time) (time.Time, error) {
	loc := time.UTC
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return time.Time{}, fmt.Errorf("unknown timezone %q", tz)
		}
		loc = l
	}
	s, err := Parser.Parse(strings.TrimSpace(expr))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}
	return s.Next(after.In(loc)).UTC(), nil
}

// Deps are the scheduler's collaborators.
type Deps struct {
	Store   store.Store
	Queue   *queue.Queue
	Planner *planner.Service
	Logger  *slog.Logger
	// Holder identifies this replica in the leader lease.
	Holder string
	// Interval between polls (default 5s).
	Interval time.Duration
	Now      func() time.Time
}

// Scheduler fires due schedule triggers.
type Scheduler struct{ d Deps }

// New returns a scheduler.
func New(d Deps) *Scheduler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Interval <= 0 {
		d.Interval = 5 * time.Second
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Scheduler{d: d}
}

// LeaseName is the KV lease of the scheduler leader.
const LeaseName = "scheduler.leader"

// Run blocks until ctx is done, polling while this replica is the leader.
func (s *Scheduler) Run(ctx context.Context) {
	s.d.Queue.RunAsLeader(ctx, LeaseName, s.d.Holder, 30*time.Second, func(lctx context.Context) {
		s.d.Logger.Info("scheduler: became leader", "holder", s.d.Holder)
		t := time.NewTicker(s.d.Interval)
		defer t.Stop()
		for {
			s.Tick(lctx)
			select {
			case <-lctx.Done():
				return
			case <-t.C:
			}
		}
	})
}

// Tick fires every due trigger once.
func (s *Scheduler) Tick(ctx context.Context) {
	now := s.d.Now()
	due, err := s.d.Store.ListDueTriggers(ctx, now, 100)
	if err != nil {
		if ctx.Err() == nil {
			s.d.Logger.Warn("scheduler: list due triggers", "err", err)
		}
		return
	}
	for _, t := range due {
		if ctx.Err() != nil {
			return
		}
		s.fire(ctx, t, now)
	}
}

func (s *Scheduler) fire(ctx context.Context, t store.Trigger, now time.Time) {
	if t.NextFireAt == nil {
		return
	}
	cfg := planner.ParseTriggerConfig(t.Config)
	var next *time.Time
	if n, err := Next(cfg.Cron, cfg.Timezone, now); err == nil {
		next = &n
	} else {
		s.d.Logger.Warn("scheduler: trigger disabled until fixed", "trigger_id", t.ID, "err", err)
	}
	ok, err := s.d.Store.MarkTriggerFired(ctx, t.ID, *t.NextFireAt, now, next)
	if err != nil || !ok {
		return // another replica claimed it, or it changed meanwhile
	}
	if next == nil {
		return
	}
	req, targets := planner.FromTrigger(t, cfg, nil, nil)
	runs, err := s.d.Planner.StartSuite(ctx, req, targets)
	if err != nil {
		s.d.Logger.Warn("scheduler: start run", "trigger_id", t.ID, "suite_id", t.SuiteID, "err", err)
		return
	}
	late := now.Sub(*t.NextFireAt)
	for _, r := range runs {
		s.d.Logger.Info("scheduler: fired", "trigger_id", t.ID, "run_id", r.ID, "late", late.Round(time.Second), "next", next)
	}
}

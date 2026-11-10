package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Job kinds.
const (
	JobExecute  = "execute"  // run target + evaluators for a shard of examples
	JobRescore  = "rescore"  // re-run evaluators on existing results
	JobFinalize = "finalize" // compute aggregates/gates (normally triggered by the last shard)
)

// Job is one unit of work on runs.jobs. Payload is owned by the engine (internal/planner, internal/runner).
type Job struct {
	Kind       string          `json:"kind"`
	RunID      string          `json:"run_id"`
	Shard      int             `json:"shard"`
	ShardCount int             `json:"shard_count"`
	Attempt    int             `json:"attempt,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// MsgID is the JetStream de-duplication id: publishing the same job twice within the
// duplicate window is a no-op.
func (j Job) MsgID() string { return fmt.Sprintf("%s.%s.%d.%d", j.RunID, j.Kind, j.Shard, j.Attempt) }

// PublishJob enqueues a job (de-duplicated by MsgID).
func (q *Queue) PublishJob(ctx context.Context, j Job) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = q.js.PublishMsg(ctx, &nats.Msg{Subject: SubjectJobs, Data: b, Header: nats.Header{
		jetstream.MsgIDHeader: []string{j.MsgID()},
	}})
	return err
}

// PublishJobs enqueues many jobs with async publishing (pipelined acks).
func (q *Queue) PublishJobs(ctx context.Context, jobs []Job) error {
	futures := make([]jetstream.PubAckFuture, 0, len(jobs))
	for _, j := range jobs {
		b, err := json.Marshal(j)
		if err != nil {
			return err
		}
		f, err := q.js.PublishMsgAsync(&nats.Msg{Subject: SubjectJobs, Data: b, Header: nats.Header{
			jetstream.MsgIDHeader: []string{j.MsgID()},
		}})
		if err != nil {
			return err
		}
		futures = append(futures, f)
	}
	for _, f := range futures {
		select {
		case <-f.Ok():
		case err := <-f.Err():
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Delivery is a received job. Handlers may call InProgress to extend the ack deadline.
type Delivery struct {
	Job Job
	Msg jetstream.Msg
}

// InProgress resets the redelivery timer for long-running jobs.
func (d Delivery) InProgress() error { return d.Msg.InProgress() }

// NumDelivered returns how many times this job has been delivered (1 = first time).
func (d Delivery) NumDelivered() uint64 {
	md, err := d.Msg.Metadata()
	if err != nil {
		return 1
	}
	return md.NumDelivered
}

// ErrNoRetry wraps handler errors that must not be redelivered (the job is terminated).
var ErrNoRetry = errors.New("queue: do not retry")

// JobHandler processes one job. Returning nil acks; ErrNoRetry terminates; other errors nak with backoff.
type JobHandler func(ctx context.Context, d Delivery) error

// ConsumeJobs pulls jobs with at most `concurrency` in flight until ctx is cancelled, then stops
// pulling, waits for in-flight handlers (they receive a context cancelled after grace) and returns.
func (q *Queue) ConsumeJobs(ctx context.Context, concurrency int, grace time.Duration, h JobHandler) error {
	if concurrency <= 0 {
		concurrency = 1
	}
	cons, err := q.js.Consumer(ctx, StreamJobs, ConsumerJobs)
	if err != nil {
		return err
	}
	// Handlers keep running for `grace` after shutdown starts so they can flush and nak cleanly.
	hctx, hcancel := context.WithCancel(context.WithoutCancel(ctx))
	defer hcancel()
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	it, err := cons.Messages(jetstream.PullMaxMessages(concurrency))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		it.Stop()
	}()
	for {
		msg, err := it.Next()
		if err != nil {
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) || ctx.Err() != nil {
				break
			}
			q.log.Warn("queue: next job", "err", err)
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(msg jetstream.Msg) {
			defer func() { <-sem; wg.Done() }()
			var j Job
			if err := json.Unmarshal(msg.Data(), &j); err != nil {
				q.log.Error("queue: bad job payload, terminating", "err", err)
				_ = msg.Term()
				return
			}
			d := Delivery{Job: j, Msg: msg}
			switch err := h(hctx, d); {
			case err == nil:
				_ = msg.Ack()
			case errors.Is(err, ErrNoRetry):
				_ = msg.TermWithReason(err.Error())
			default:
				delay := time.Duration(d.NumDelivered()) * 2 * time.Second
				q.log.Warn("queue: job failed, will retry", "run_id", j.RunID, "shard", j.Shard, "err", err, "delay", delay)
				_ = msg.NakWithDelay(delay)
			}
		}(msg)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(grace):
		hcancel() // tell handlers to stop; they should nak remaining work
		<-done
	}
	return nil
}

// PendingJobs returns the number of jobs not yet acknowledged (queue lag, for metrics/KEDA).
func (q *Queue) PendingJobs(ctx context.Context) (uint64, error) {
	c, err := q.js.Consumer(ctx, StreamJobs, ConsumerJobs)
	if err != nil {
		return 0, err
	}
	info, err := c.Info(ctx)
	if err != nil {
		return 0, err
	}
	return info.NumPending + uint64(info.NumAckPending), nil
}

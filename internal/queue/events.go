package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Event types published on runs.events.<runID>. Data shapes are owned by the engine; the API's
// SSE endpoint forwards them verbatim (event: <Type>, data: <Data>).
const (
	EventRunStatus = "run.status"   // {"status": "..."}
	EventProgress  = "run.progress" // {"completed": n, "failed": n, "errored": n, "total": n}
	EventResult    = "result"       // compact result row
	EventFinished  = "run.finished" // final summary
)

// Event is a run progress event.
type Event struct {
	Seq   uint64          `json:"seq,omitempty"` // stream sequence (set on receive)
	Type  string          `json:"type"`
	RunID string          `json:"run_id"`
	At    time.Time       `json:"at"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// PublishEvent publishes an event (fire-and-forget async publish; ordering per publisher is kept).
func (q *Queue) PublishEvent(ctx context.Context, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = q.js.PublishAsync(EventsSubject(e.RunID), b)
	return err
}

// SubscribeEvents streams a run's events. With fromSeq > 0 it resumes after that sequence
// (SSE Last-Event-ID); with fromSeq == 0 it replays all retained events. The channel closes
// when ctx is done.
func (q *Queue) SubscribeEvents(ctx context.Context, runID string, fromSeq uint64) (<-chan Event, error) {
	cfg := jetstream.OrderedConsumerConfig{FilterSubjects: []string{EventsSubject(runID)}}
	if fromSeq > 0 {
		cfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		cfg.OptStartSeq = fromSeq + 1
	}
	cons, err := q.js.OrderedConsumer(ctx, StreamEvents, cfg)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 256)
	cc, err := cons.Consume(func(m jetstream.Msg) {
		var e Event
		if json.Unmarshal(m.Data(), &e) != nil {
			return
		}
		if md, err := m.Metadata(); err == nil {
			e.Seq = md.Sequence.Stream
		}
		select {
		case out <- e:
		case <-ctx.Done():
		}
	})
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		cc.Stop()
		<-cc.Closed()
		close(out)
	}()
	return out, nil
}

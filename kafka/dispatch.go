package kafka

import (
	"context"
	"hash/fnv"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
)

// fetchControl is the part of *kgo.Client the dispatcher uses.
type fetchControl interface {
	MarkCommitOffsets(map[string]map[int32]kgo.EpochOffset)
	PauseFetchPartitions(map[string][]int32) map[string][]int32
	ResumeFetchPartitions(map[string][]int32)
}

type topicPartition struct {
	topic     string
	partition int32
}

func (tp topicPartition) asMap() map[string][]int32 {
	return map[string][]int32{tp.topic: {tp.partition}}
}

// dispatcher settles records concurrently while keeping each account's records in order: records with
// the same key always go to the same worker. A partition is committed only up to its oldest unsettled
// record, so a crash re-reads whatever wasn't settled.
type dispatcher struct {
	control    fetchControl
	settle     func(context.Context, *kgo.Record) bool
	maxPending int
	workers    []*worker
	ctx        context.Context
	cancel     context.CancelFunc
	done       sync.WaitGroup

	mu    sync.Mutex
	parts map[topicPartition]*partition
}

type partition struct {
	tp      topicPartition
	ctx     context.Context
	cancel  context.CancelFunc
	pending []*entry // in offset order
	paused  bool
	revoked bool
	started sync.WaitGroup
}

type entry struct {
	rec     *kgo.Record
	settled bool
}

type job struct {
	part  *partition
	entry *entry
}

func newDispatcher(control fetchControl, settle func(context.Context, *kgo.Record) bool, workers, maxPending int) *dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	d := &dispatcher{
		control:    control,
		settle:     settle,
		maxPending: maxPending,
		ctx:        ctx,
		cancel:     cancel,
		parts:      map[topicPartition]*partition{},
	}
	for range workers {
		w := &worker{wake: make(chan struct{}, 1)}
		d.workers = append(d.workers, w)
		d.done.Go(func() {
			for {
				j, ok := w.next(ctx)
				if !ok {
					return
				}
				d.process(j)
			}
		})
	}
	return d
}

func (d *dispatcher) close() {
	d.cancel()
	d.done.Wait()
}

func (d *dispatcher) assign(partitions map[string][]int32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for topic, ps := range partitions {
		for _, p := range ps {
			tp := topicPartition{topic, p}
			ctx, cancel := context.WithCancel(d.ctx)
			d.parts[tp] = &partition{tp: tp, ctx: ctx, cancel: cancel}
		}
	}
}

// revoke cancels the partitions' records in progress and waits for them, but not for queued ones,
// which workers drop when they reach them; so it never waits behind another partition's slow records.
func (d *dispatcher) revoke(partitions map[string][]int32) {
	var stopping []*partition
	d.mu.Lock()
	for topic, ps := range partitions {
		for _, p := range ps {
			tp := topicPartition{topic, p}
			if part, ok := d.parts[tp]; ok {
				part.revoked = true
				delete(d.parts, tp)
				stopping = append(stopping, part)
			}
		}
	}
	d.mu.Unlock()

	for _, part := range stopping {
		part.cancel()
	}
	for _, part := range stopping {
		part.started.Wait()
		if part.paused {
			// Pauses outlive ownership, so the partition's next owner here would never fetch it.
			d.control.ResumeFetchPartitions(part.tp.asMap())
		}
	}
}

// dispatch must not run concurrently with revoke; the consumer blocks rebalances while dispatching.
func (d *dispatcher) dispatch(topic string, partition int32, recs []*kgo.Record) {
	d.mu.Lock()
	part, ok := d.parts[topicPartition{topic, partition}]
	if !ok {
		d.mu.Unlock()
		return
	}
	jobs := make([]job, len(recs))
	for i, rec := range recs {
		e := &entry{rec: rec}
		part.pending = append(part.pending, e)
		jobs[i] = job{part: part, entry: e}
	}
	if !part.paused && len(part.pending) >= d.maxPending {
		d.control.PauseFetchPartitions(part.tp.asMap())
		part.paused = true
	}
	d.mu.Unlock()

	for _, j := range jobs {
		d.workerFor(j.entry.rec.Key).push(j)
	}
}

func (d *dispatcher) workerFor(key []byte) *worker {
	h := fnv.New32a()
	h.Write(key)
	return d.workers[h.Sum32()%uint32(len(d.workers))]
}

func (d *dispatcher) process(j job) {
	d.mu.Lock()
	if j.part.revoked {
		d.mu.Unlock()
		return
	}
	j.part.started.Add(1)
	d.mu.Unlock()
	defer j.part.started.Done()

	settled := d.settle(j.part.ctx, j.entry.rec)

	d.mu.Lock()
	defer d.mu.Unlock()
	if settled && !j.part.revoked {
		j.entry.settled = true
		d.advance(j.part)
	}
}

// advance commits past the settled records at the front of the partition and resumes fetching once
// there is room again. Called with d.mu held.
func (d *dispatcher) advance(part *partition) {
	var last *kgo.Record
	for len(part.pending) > 0 && part.pending[0].settled {
		last = part.pending[0].rec
		part.pending[0] = nil
		part.pending = part.pending[1:]
	}
	if last != nil {
		d.control.MarkCommitOffsets(map[string]map[int32]kgo.EpochOffset{
			last.Topic: {last.Partition: {Epoch: last.LeaderEpoch, Offset: last.Offset + 1}},
		})
	}
	if part.paused && len(part.pending) < d.maxPending {
		d.control.ResumeFetchPartitions(part.tp.asMap())
		part.paused = false
	}
}

type worker struct {
	mu    sync.Mutex
	queue []job
	wake  chan struct{}
}

func (w *worker) push(j job) {
	w.mu.Lock()
	w.queue = append(w.queue, j)
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *worker) next(ctx context.Context) (job, bool) {
	for {
		w.mu.Lock()
		if len(w.queue) > 0 {
			j := w.queue[0]
			w.queue[0] = job{}
			w.queue = w.queue[1:]
			w.mu.Unlock()
			return j, true
		}
		w.mu.Unlock()
		select {
		case <-w.wake:
		case <-ctx.Done():
			return job{}, false
		}
	}
}

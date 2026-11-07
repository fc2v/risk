package kafka

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type fakeControl struct {
	mu        sync.Mutex
	committed map[topicPartition]int64
	paused    map[topicPartition]bool
}

func newFakeControl() *fakeControl {
	return &fakeControl{committed: map[topicPartition]int64{}, paused: map[topicPartition]bool{}}
}

func (f *fakeControl) MarkCommitOffsets(m map[string]map[int32]kgo.EpochOffset) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for topic, ps := range m {
		for p, eo := range ps {
			f.committed[topicPartition{topic, p}] = eo.Offset
		}
	}
}

func (f *fakeControl) PauseFetchPartitions(m map[string][]int32) map[string][]int32 {
	f.set(m, true)
	return m
}

func (f *fakeControl) ResumeFetchPartitions(m map[string][]int32) { f.set(m, false) }

func (f *fakeControl) set(m map[string][]int32, paused bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for topic, ps := range m {
		for _, p := range ps {
			f.paused[topicPartition{topic, p}] = paused
		}
	}
}

func (f *fakeControl) committedAt(tp topicPartition) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.committed[tp]
}

func (f *fakeControl) isPaused(tp topicPartition) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paused[tp]
}

// fakeSettle settles instantly, except for held accounts, which wait until released or cancelled.
type fakeSettle struct {
	mu      sync.Mutex
	held    map[string]chan struct{}
	settled map[string][]int64
	started chan string
}

func newFakeSettle(held ...string) *fakeSettle {
	f := &fakeSettle{held: map[string]chan struct{}{}, settled: map[string][]int64{}, started: make(chan string, 100)}
	for _, k := range held {
		f.held[k] = make(chan struct{})
	}
	return f
}

func (f *fakeSettle) settle(ctx context.Context, rec *kgo.Record) bool {
	f.started <- string(rec.Key)
	f.mu.Lock()
	gate := f.held[string(rec.Key)]
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return false
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settled[string(rec.Key)] = append(f.settled[string(rec.Key)], rec.Offset)
	return true
}

func (f *fakeSettle) release(key string) { close(f.held[key]) }

func (f *fakeSettle) settledFor(key string) []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.settled[key])
}

func (f *fakeSettle) waitStarted(t *testing.T, key string) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case k := <-f.started:
			if k == key {
				return
			}
		case <-timeout:
			t.Fatalf("%s never started settling", key)
		}
	}
}

const topic = "txs.incoming"

var p0 = topicPartition{topic, 0}

func recs(partition int32, keys ...string) []*kgo.Record {
	out := make([]*kgo.Record, len(keys))
	for i, k := range keys {
		out[i] = &kgo.Record{Topic: topic, Partition: partition, Offset: int64(i), Key: []byte(k)}
	}
	return out
}

// keysOnOtherWorkers returns n keys that don't share a worker with avoid.
func keysOnOtherWorkers(d *dispatcher, avoid string, n int) []string {
	var keys []string
	for i := 0; len(keys) < n; i++ {
		k := fmt.Sprintf("acc-%d", i)
		if d.workerFor([]byte(k)) != d.workerFor([]byte(avoid)) {
			keys = append(keys, k)
		}
	}
	return keys
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func newTestDispatcher(t *testing.T, settle *fakeSettle, maxPending int) (*dispatcher, *fakeControl) {
	t.Helper()
	control := newFakeControl()
	d := newDispatcher(control, settle.settle, 8, maxPending)
	t.Cleanup(d.close)
	d.assign(map[string][]int32{topic: {0, 1}})
	return d, control
}

func TestHotAccountDoesNotDelayOthers(t *testing.T) {
	settle := newFakeSettle("hot")
	d, control := newTestDispatcher(t, settle, 1000)
	others := keysOnOtherWorkers(d, "hot", 3)

	d.dispatch(topic, 0, recs(0, "hot", others[0], others[1], "hot", others[2]))

	eventually(t, "other accounts to settle while hot is held", func() bool {
		return len(settle.settledFor(others[0])) == 1 && len(settle.settledFor(others[1])) == 1 && len(settle.settledFor(others[2])) == 1
	})
	if got := control.committedAt(p0); got != 0 {
		t.Errorf("committed up to %d while offset 0 (hot) is unsettled, want nothing committed", got)
	}

	settle.release("hot")
	eventually(t, "all five records to be committed", func() bool { return control.committedAt(p0) == 5 })
	if got := settle.settledFor("hot"); !slices.Equal(got, []int64{0, 3}) {
		t.Errorf("hot settled offsets %v, want [0 3] in order", got)
	}
}

func TestAccountRecordsSettleInOrder(t *testing.T) {
	settle := newFakeSettle()
	d, control := newTestDispatcher(t, settle, 1000)

	keys := make([]string, 50)
	for i := range keys {
		keys[i] = []string{"a", "b"}[i%2]
	}
	d.dispatch(topic, 0, recs(0, keys...))

	eventually(t, "every record to be committed", func() bool { return control.committedAt(p0) == 50 })
	for _, k := range []string{"a", "b"} {
		got := settle.settledFor(k)
		if !slices.IsSorted(got) || len(got) != 25 {
			t.Errorf("account %s settled %v, want 25 offsets in order", k, got)
		}
	}
}

func TestPausesAtCapAndResumes(t *testing.T) {
	settle := newFakeSettle("hot")
	d, control := newTestDispatcher(t, settle, 3)

	d.dispatch(topic, 0, recs(0, "hot", "hot", "hot"))
	if !control.isPaused(p0) {
		t.Fatal("partition not paused at 3 pending records with a cap of 3")
	}

	settle.release("hot")
	eventually(t, "partition to resume", func() bool { return !control.isPaused(p0) })
}

func TestRevokeCancelsInProgressAndDropsQueued(t *testing.T) {
	settle := newFakeSettle("hot")
	d, control := newTestDispatcher(t, settle, 2)

	d.dispatch(topic, 0, recs(0, "hot", "hot"))
	settle.waitStarted(t, "hot")
	if !control.isPaused(p0) {
		t.Fatal("partition not paused at the cap")
	}

	done := make(chan struct{})
	go func() {
		d.revoke(map[string][]int32{topic: {0}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("revoke didn't return: in-progress record wasn't cancelled")
	}

	if got := settle.settledFor("hot"); len(got) != 0 {
		t.Errorf("revoked partition settled %v, want nothing", got)
	}
	if got := control.committedAt(p0); got != 0 {
		t.Errorf("revoked partition committed to %d, want nothing", got)
	}
	if control.isPaused(p0) {
		t.Error("revoked partition left paused; its next owner here would never fetch it")
	}
	d.dispatch(topic, 0, recs(0, "late"))
	time.Sleep(20 * time.Millisecond)
	if got := settle.settledFor("late"); len(got) != 0 {
		t.Error("record for a revoked partition was settled")
	}
}

func TestRevokeDoesNotWaitBehindOtherPartitions(t *testing.T) {
	settle := newFakeSettle("hot")
	d, _ := newTestDispatcher(t, settle, 1000)

	d.dispatch(topic, 1, recs(1, "hot"))
	settle.waitStarted(t, "hot")
	d.dispatch(topic, 0, recs(0, "hot"))

	done := make(chan struct{})
	go func() {
		d.revoke(map[string][]int32{topic: {0}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("revoking partition 0 waited for partition 1's held record")
	}

	settle.release("hot")
	eventually(t, "partition 1's record to settle", func() bool { return len(settle.settledFor("hot")) == 1 })
	time.Sleep(20 * time.Millisecond)
	if got := settle.settledFor("hot"); len(got) != 1 {
		t.Errorf("hot settled %d records, want 1: partition 0's queued record must be dropped", len(got))
	}
}

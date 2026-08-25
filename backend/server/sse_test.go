package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/hertz-contrib/sse"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestSSEReplay_DedupsAndTracksWatermark(t *testing.T) {
	base := time.Now()
	r := newSSEReplay([]*entity.MagiEvent{
		{ID: "e1", CaseID: "c1", Seq: 1, Timestamp: base},
		{ID: "e2", CaseID: "c1", Seq: 2, Timestamp: base.Add(1 * time.Second)},
	})
	if !r.isNew(&entity.MagiEvent{ID: "e3", Seq: 3}) {
		t.Fatal("e3 should be new")
	}
	if r.isNew(&entity.MagiEvent{ID: "e1"}) {
		t.Fatal("e1 should be marked delivered")
	}
	if r.lastSeq != 2 {
		t.Fatalf("watermark = %d, want 2", r.lastSeq)
	}
}

func TestParseLastEventID_RequiresUnsignedSequence(t *testing.T) {
	if got, err := parseLastEventID("7"); err != nil || got != 7 {
		t.Fatalf("parse 7 = (%d, %v), want (7, nil)", got, err)
	}
	for _, raw := range []string{"-1", "seven", "7.0"} {
		if _, err := parseLastEventID(raw); err == nil {
			t.Fatalf("parse %q should fail", raw)
		}
	}
}

type fakeEventRepo struct {
	mu      sync.Mutex
	events  []*entity.MagiEvent
	listErr error
}

type captureExtWriter struct{ bytes.Buffer }

func (w *captureExtWriter) Flush() error    { return nil }
func (w *captureExtWriter) Finalize() error { return nil }

func (f *fakeEventRepo) setEvents(events []*entity.MagiEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append([]*entity.MagiEvent(nil), events...)
}

func (f *fakeEventRepo) addEvent(event *entity.MagiEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeEventRepo) snapshot() []*entity.MagiEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*entity.MagiEvent(nil), f.events...)
}

func (f *fakeEventRepo) Create(ctx context.Context, e *entity.MagiEvent) error { return nil }
func (f *fakeEventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	return f.snapshot(), nil
}
func (f *fakeEventRepo) ListAfter(ctx context.Context, caseID string, after time.Time) ([]*entity.MagiEvent, error) {
	return nil, errors.New("timestamp-based event replay is unsupported")
}
func (f *fakeEventRepo) ListAfterSeq(ctx context.Context, caseID string, afterSeq uint64, limit int) ([]*entity.MagiEvent, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*entity.MagiEvent
	for _, e := range f.snapshot() {
		if e.Seq > afterSeq {
			out = append(out, e)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func TestPollOnce_UsesSequenceWatermark(t *testing.T) {
	repo := &fakeEventRepo{}
	base := time.Now()
	repo.setEvents([]*entity.MagiEvent{
		// Deliberately make timestamps disagree with durable sequence order.
		{ID: "8", CaseID: "c1", Seq: 8, Timestamp: base.Add(2 * time.Second)},
		{ID: "9", CaseID: "c1", Seq: 9, Timestamp: base},
		{ID: "10", CaseID: "c1", Seq: 10, Timestamp: base.Add(time.Second)},
	})
	replay := newSSEReplay([]*entity.MagiEvent{{ID: "7", CaseID: "c1", Seq: 7}})

	var emitted []string
	if err := pollOnce(context.Background(), repo, "c1", replay, func(ev *entity.MagiEvent) error {
		emitted = append(emitted, ev.ID)
		return nil
	}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got, want := strings.Join(emitted, ","), "8,9,10"; got != want {
		t.Fatalf("emitted = %q, want %q", got, want)
	}
}

func TestPollOnce_DedupsBrokerAndPersistedEvent(t *testing.T) {
	repo := &fakeEventRepo{}
	repo.setEvents([]*entity.MagiEvent{{ID: "8", CaseID: "c1", Seq: 8}})
	replay := newSSEReplay([]*entity.MagiEvent{{ID: "7", CaseID: "c1", Seq: 7}})
	var emitted []string
	if err := pollOnce(context.Background(), repo, "c1", replay, func(ev *entity.MagiEvent) error {
		emitted = append(emitted, ev.ID)
		return nil
	}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if replay.isNew(&entity.MagiEvent{ID: "8", CaseID: "c1", Seq: 8}) {
		t.Fatal("persisted event should be suppressed when it arrives from broker")
	}
	if got := strings.Join(emitted, ","); got != "8" {
		t.Fatalf("emitted = %q, want 8", got)
	}
}

func TestForwardBrokerEvent_DoesNotCatchUpPastBrokerEvent(t *testing.T) {
	repo := &fakeEventRepo{}
	repo.setEvents([]*entity.MagiEvent{
		{ID: "8", CaseID: "c1", Seq: 8},
		{ID: "9", CaseID: "c1", Seq: 9},
		{ID: "11", CaseID: "c1", Seq: 11},
	})
	replay := newSSEReplay([]*entity.MagiEvent{{ID: "7", CaseID: "c1", Seq: 7}})
	writer := &captureExtWriter{}
	stream := sse.NewStreamWithWriter(app.NewContext(0), writer)
	if !forwardBrokerEvent(context.Background(), repo, "c1", replay, stream, &entity.MagiEvent{ID: "10", CaseID: "c1", Seq: 10}) {
		t.Fatal("forward should remain open")
	}
	var got []string
	for _, line := range strings.Split(writer.String(), "\n") {
		if strings.HasPrefix(line, "id:") {
			got = append(got, strings.TrimSpace(strings.TrimPrefix(line, "id:")))
		}
	}
	if got := strings.Join(got, ","); got != "8,9,10" {
		t.Fatalf("emitted IDs = %q, want 8,9,10", got)
	}
}

func TestForwardBrokerEvent_ClosesOnCatchUpError(t *testing.T) {
	repo := &fakeEventRepo{listErr: errors.New("store unavailable")}
	replay := newSSEReplay([]*entity.MagiEvent{{ID: "7", CaseID: "c1", Seq: 7}})
	if forwardBrokerEvent(context.Background(), repo, "c1", replay, nil, &entity.MagiEvent{ID: "10", CaseID: "c1", Seq: 10}) {
		t.Fatal("catch-up error must not consume the broker event")
	}
}

func TestPollOnce_ForwardsCrossInstanceEvents(t *testing.T) {
	repo := &fakeEventRepo{}
	base := time.Now()
	// Events that existed before the connection: history is replayed on
	// connect, so the poller must not re-emit them.
	repo.setEvents([]*entity.MagiEvent{
		{ID: "old1", CaseID: "c1", Seq: 1, Timestamp: base},
		{ID: "old2", CaseID: "c1", Seq: 2, Timestamp: base.Add(time.Second)},
	})
	replay := newSSEReplay(repo.snapshot())

	var emitted []string
	emit := func(ev *entity.MagiEvent) error {
		emitted = append(emitted, ev.ID)
		return nil
	}

	// A remote instance persists new events after the subscriber connected.
	repo.addEvent(&entity.MagiEvent{ID: "remote1", CaseID: "c1", Seq: 3, Timestamp: base.Add(2 * time.Second)})
	repo.addEvent(&entity.MagiEvent{ID: "remote2", CaseID: "c1", Seq: 4, Timestamp: base.Add(3 * time.Second)})

	if err := pollOnce(context.Background(), repo, "c1", replay, emit); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(emitted) != 2 || emitted[0] != "remote1" || emitted[1] != "remote2" {
		t.Fatalf("emitted = %v, want [remote1 remote2]", emitted)
	}

	// A second poll with no new events must emit nothing (watermark advanced).
	emitted = nil
	if err := pollOnce(context.Background(), repo, "c1", replay, emit); err != nil {
		t.Fatalf("poll2: %v", err)
	}
	if len(emitted) != 0 {
		t.Fatalf("second poll emitted %v, want none", emitted)
	}
}

func TestPollOnce_SurfacesStoreErrors(t *testing.T) {
	repo := &fakeEventRepo{listErr: errors.New("store unreachable")}
	replay := newSSEReplay(nil)
	if err := pollOnce(context.Background(), repo, "c1", replay, func(*entity.MagiEvent) error { return nil }); err == nil {
		t.Fatal("expected store error to surface")
	}
}

// sseHTTPHarness starts a real Hertz server exposing the SSE route on a free
// port and returns its base URL.
func sseHTTPHarness(t *testing.T, broker *EventBroker, repo port.EventRepository) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	h := hzserver.Default(hzserver.WithHostPorts(addr))
	h.GET("/api/v1/cases/:id/stream", SSEHandlerWithHistory(broker, repo, nil))
	go func() { h.Spin() }()
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	// Wait until the listener is accepting connections (Spin is async).
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "http://" + addr
}

func TestSSE_CrossInstancePollerDeliversRemoteEvents(t *testing.T) {
	old := crossInstancePollInterval
	crossInstancePollInterval = 50 * time.Millisecond
	t.Cleanup(func() { crossInstancePollInterval = old })

	broker := NewEventBroker()
	repo := &fakeEventRepo{}
	base := time.Now()
	repo.setEvents([]*entity.MagiEvent{
		{ID: "hist1", CaseID: "c1", Seq: 1, Timestamp: base},
	})

	baseURL := sseHTTPHarness(t, broker, repo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/cases/c1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 512*1024)

	// readLines scans until it has seen the expected event IDs or the context
	// is cancelled, buffering lines.
	var got []string
	deadline := time.After(5 * time.Second)
	readOne := func() bool {
		select {
		case <-deadline:
			return false
		default:
		}
		if !sc.Scan() {
			return false
		}
		line := sc.Text()
		if strings.HasPrefix(line, "id:") {
			got = append(got, strings.TrimSpace(strings.TrimPrefix(line, "id:")))
		}
		return true
	}

	// wait until history id arrives.
	waitID := func(id string) bool {
		for i := 0; i < 200; i++ {
			for _, g := range got {
				if g == id {
					return true
				}
			}
			if !readOne() {
				time.Sleep(20 * time.Millisecond)
			}
		}
		return false
	}

	if !waitID("1") {
		t.Fatalf("history event not delivered; got %v", got)
	}

	// Now simulate another instance: publish a local event through the broker
	// AND persist a remote event only visible via the DB poller.
	broker.Publish(context.Background(), entity.MagiEvent{ID: "local1", CaseID: "c1", Seq: 2, Timestamp: base.Add(time.Second)})
	repo.addEvent(&entity.MagiEvent{ID: "remote1", CaseID: "c1", Seq: 3, Timestamp: base.Add(2 * time.Second)})

	if !waitID("2") {
		t.Fatalf("local event not delivered; got %v", got)
	}
	if !waitID("3") {
		t.Fatalf("remote (cross-instance) event not delivered via poller; got %v", got)
	}
}

func TestSSE_LastEventIDResumesBySequence(t *testing.T) {
	browser := NewEventBroker()
	repo := &fakeEventRepo{}
	base := time.Now()
	var history []*entity.MagiEvent
	for seq := uint64(1); seq <= 10; seq++ {
		history = append(history, &entity.MagiEvent{
			ID: strconv.FormatUint(seq, 10), CaseID: "c1", Seq: seq,
			Timestamp: base.Add(time.Duration(10-seq) * time.Second),
		})
	}
	repo.setEvents(history)
	baseURL := sseHTTPHarness(t, browser, repo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/cases/c1/stream", nil)
	req.Header.Set("Last-Event-ID", "7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	sc := bufio.NewScanner(resp.Body)
	var got []string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "id:") {
			got = append(got, strings.TrimSpace(strings.TrimPrefix(line, "id:")))
			if len(got) == 3 {
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}
	if got := strings.Join(got, ","); got != "8,9,10" {
		t.Fatalf("replayed IDs = %q, want %q", got, "8,9,10")
	}
}

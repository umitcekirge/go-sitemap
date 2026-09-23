package sitemap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestNotificationDisabledByDefault(t *testing.T) {
	g, _ := newGen(t, Options{})
	res, err := g.Generate(context.Background(), makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(res.Notifications) != 0 {
		t.Fatalf("no notifications should occur by default, got %d", len(res.Notifications))
	}
}

func TestIndexNowNotifier(t *testing.T) {
	var calls int32
	var gotPayload indexNowPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &IndexNowNotifier{
		Key:      "secretkey123",
		Host:     "example.com",
		Endpoint: srv.URL,
		Client:   srv.Client(),
		URLs:     []string{"https://example.com/changed-1", "https://example.com/changed-2"},
	}
	g, _ := newGen(t, Options{Notifiers: []Notifier{n}})
	res, err := g.Generate(context.Background(), makeProvider("pages", 1))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("want 1 IndexNow call, got %d", calls)
	}
	if gotPayload.Host != "example.com" || gotPayload.Key != "secretkey123" {
		t.Fatalf("payload host/key wrong: %+v", gotPayload)
	}
	if len(gotPayload.URLList) != 2 {
		t.Fatalf("want 2 urls submitted, got %d", len(gotPayload.URLList))
	}
	if len(res.Notifications) != 1 || res.Notifications[0].Err != nil {
		t.Fatalf("notification result wrong: %+v", res.Notifications)
	}
}

func TestIndexNowBatching(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	urls := make([]string, 25)
	for i := range urls {
		urls[i] = "https://example.com/p/" + strconv.Itoa(i)
	}
	n := &IndexNowNotifier{Key: "k", Host: "example.com", Endpoint: srv.URL, Client: srv.Client(), BatchSize: 10, URLs: urls}
	if err := n.Notify(context.Background(), nil); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("want 3 batches, got %d", calls)
	}
}

func TestIndexNowRequiresKeyAndHost(t *testing.T) {
	n := &IndexNowNotifier{URLs: []string{"https://e.com/x"}}
	if err := n.Notify(context.Background(), nil); err == nil {
		t.Fatalf("missing key/host should error")
	}
}

func TestLegacyPingNotifier(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Query().Get("sitemap") == "" {
			t.Errorf("expected sitemap query param")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &LegacyPingNotifier{Endpoints: []string{srv.URL + "/ping?sitemap=%s"}, Client: srv.Client()}
	if err := n.Notify(context.Background(), []string{"https://example.com/sitemap-index.xml"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("want 1 ping, got %d", calls)
	}
}

func TestLegacyPingRequiresPlaceholder(t *testing.T) {
	n := &LegacyPingNotifier{Endpoints: []string{"https://example.com/ping"}}
	if err := n.Notify(context.Background(), []string{"https://example.com/sitemap.xml"}); err == nil {
		t.Fatalf("endpoint without %%s placeholder should error")
	}
}

func TestMultiNotifierCollectsErrors(t *testing.T) {
	ok := NotifierFunc{NotifierName: "ok", Fn: func(context.Context, []string) error { return nil }}
	bad := NotifierFunc{NotifierName: "bad", Fn: func(context.Context, []string) error { return errFake }}
	m := &MultiNotifier{Notifiers: []Notifier{ok, bad}}
	if err := m.Notify(context.Background(), []string{"https://e.com/s.xml"}); err == nil {
		t.Fatalf("multi notifier should report the failing child")
	}
}

var errFake = errFakeType("boom")

type errFakeType string

func (e errFakeType) Error() string { return string(e) }

func TestIndexNowSkipsWithoutURLs(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	n := &IndexNowNotifier{Key: "k", Host: "example.com", Endpoint: srv.URL, Client: srv.Client()}
	if err := n.Notify(context.Background(), []string{"https://example.com/sitemap-index.xml"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if calls != 0 {
		t.Fatalf("sitemap URLs must not be submitted to IndexNow")
	}
}

func TestLegacyPingKeepsPercentEscapes(t *testing.T) {
	var gotSrc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSrc = r.URL.Query().Get("src")
	}))
	defer srv.Close()
	n := &LegacyPingNotifier{Endpoints: []string{srv.URL + "/ping?src=a%2Fb&sitemap=%s"}, Client: srv.Client()}
	if err := n.Notify(context.Background(), []string{"https://example.com/s.xml"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if gotSrc != "a/b" {
		t.Fatalf("src = %q, want a/b", gotSrc)
	}
}

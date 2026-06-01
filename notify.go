package sitemap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Notifier is an optional hook invoked after successful generation with the
// public URLs of the generated sitemap index file(s). No notifier runs unless
// explicitly configured, and none run on DryRun.
//
// Modern guidance: prefer Search Console verification and a robots.txt Sitemap
// declaration for Google discovery. IndexNow is the recommended programmatic
// option for participating engines. The legacy GET "ping" endpoints are
// deprecated and provided only as an opt-in compatibility shim.
type Notifier interface {
	// Name identifies the notifier in Result.Notifications.
	Name() string
	// Notify is called with the index URLs (or, when no index is produced, the
	// individual sitemap URLs).
	Notify(ctx context.Context, sitemapURLs []string) error
}

// httpDoer is the minimal HTTP client surface, satisfied by *http.Client. Tests
// inject a client pointing at httptest servers; no real network calls happen by
// default.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// MultiNotifier fans a notification out to several notifiers, collecting all
// errors instead of stopping at the first.
type MultiNotifier struct {
	Notifiers []Notifier
}

// Name implements Notifier.
func (m *MultiNotifier) Name() string { return "multi" }

// Notify implements Notifier, invoking each child and joining errors.
func (m *MultiNotifier) Notify(ctx context.Context, sitemapURLs []string) error {
	var msgs []string
	for _, n := range m.Notifiers {
		if n == nil {
			continue
		}
		if err := n.Notify(ctx, sitemapURLs); err != nil {
			msgs = append(msgs, fmt.Sprintf("%s: %v", n.Name(), err))
		}
	}
	if len(msgs) > 0 {
		return newErr(ErrNotify, "notify", strings.Join(msgs, "; "))
	}
	return nil
}

// IndexNowNotifier submits changed URLs to an IndexNow endpoint
// (https://www.indexnow.org). It is disabled unless constructed and added to
// Options.Notifiers. It submits in batches via the JSON API.
type IndexNowNotifier struct {
	// Key is the IndexNow API key. Required.
	Key string
	// Host is the site host the key authorises, e.g. "example.com". Required.
	Host string
	// KeyLocation is the absolute URL of the hosted key file. Optional; when
	// empty IndexNow assumes "https://<host>/<key>.txt".
	KeyLocation string
	// Endpoint overrides the IndexNow submission endpoint. Zero value ->
	// "https://api.indexnow.org/indexnow".
	Endpoint string
	// BatchSize caps URLs per request. Zero -> 10000 (the IndexNow maximum).
	BatchSize int
	// Client is the HTTP client. Zero value -> a client with a 30s timeout.
	Client httpDoer
	// URLs, when set, is submitted instead of the generated sitemap URLs. Set
	// this to the list of content URLs that actually changed (recommended).
	URLs []string
}

// Name implements Notifier.
func (n *IndexNowNotifier) Name() string { return "indexnow" }

// indexNowPayload is the JSON body of an IndexNow batch submission.
type indexNowPayload struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation,omitempty"`
	URLList     []string `json:"urlList"`
}

// Notify implements Notifier. When URLs is set those are submitted; otherwise
// the provided sitemap URLs are used as a fallback.
func (n *IndexNowNotifier) Notify(ctx context.Context, sitemapURLs []string) error {
	if n.Key == "" || n.Host == "" {
		return newErr(ErrNotify, "indexnow", "Key and Host are required")
	}
	urls := n.URLs
	if len(urls) == 0 {
		urls = sitemapURLs
	}
	if len(urls) == 0 {
		return nil
	}

	endpoint := n.Endpoint
	if endpoint == "" {
		endpoint = "https://api.indexnow.org/indexnow"
	}
	batch := n.BatchSize
	if batch <= 0 {
		batch = 10000
	}
	client := n.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	for start := 0; start < len(urls); start += batch {
		end := min(start+batch, len(urls))
		payload := indexNowPayload{
			Host:        n.Host,
			Key:         n.Key,
			KeyLocation: n.KeyLocation,
			URLList:     urls[start:end],
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return newErr(ErrNotify, "indexnow", "could not encode payload").wrap(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return newErr(ErrNotify, "indexnow", "could not build request").wrap(err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := client.Do(req)
		if err != nil {
			return newErr(ErrNotify, "indexnow", "request failed").wrap(err)
		}
		// Drain and close so connections can be reused.
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return newErr(ErrNotify, "indexnow",
				fmt.Sprintf("endpoint returned status %d", resp.StatusCode))
		}
	}
	return nil
}

// LegacyPingNotifier performs the deprecated GET "ping" against explicitly
// configured endpoints. It is OFF by default and MUST NOT be used to notify
// Google: Google removed its ping endpoint. Each Endpoint must contain a
// "%s" placeholder for the URL-encoded sitemap URL, e.g.
// "https://www.bing.com/ping?sitemap=%s". Provided only for compatibility with
// niche consumers that still support it.
type LegacyPingNotifier struct {
	// Endpoints are printf templates with a single %s for the encoded URL.
	Endpoints []string
	// Client is the HTTP client. Zero value -> a client with a 30s timeout.
	Client httpDoer
}

// Name implements Notifier.
func (n *LegacyPingNotifier) Name() string { return "legacy-ping" }

// Notify implements Notifier by GETting each endpoint for each sitemap URL.
func (n *LegacyPingNotifier) Notify(ctx context.Context, sitemapURLs []string) error {
	if len(n.Endpoints) == 0 {
		return nil
	}
	client := n.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	for _, tmpl := range n.Endpoints {
		if !strings.Contains(tmpl, "%s") {
			return newErr(ErrNotify, "legacy-ping",
				fmt.Sprintf("endpoint %q must contain a %%s placeholder", tmpl))
		}
		for _, su := range sitemapURLs {
			target := fmt.Sprintf(tmpl, url.QueryEscape(su))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return newErr(ErrNotify, "legacy-ping", "could not build request").wrap(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				return newErr(ErrNotify, "legacy-ping", "request failed").wrap(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return newErr(ErrNotify, "legacy-ping",
					fmt.Sprintf("endpoint returned status %d", resp.StatusCode))
			}
		}
	}
	return nil
}

// NotifierFunc adapts a function to the Notifier interface for custom hooks.
type NotifierFunc struct {
	NotifierName string
	Fn           func(ctx context.Context, sitemapURLs []string) error
}

// Name implements Notifier.
func (f NotifierFunc) Name() string {
	if f.NotifierName == "" {
		return "custom"
	}
	return f.NotifierName
}

// Notify implements Notifier.
func (f NotifierFunc) Notify(ctx context.Context, sitemapURLs []string) error {
	if f.Fn == nil {
		return nil
	}
	return f.Fn(ctx, sitemapURLs)
}

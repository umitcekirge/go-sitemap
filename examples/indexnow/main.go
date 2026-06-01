// Command indexnow demonstrates opt-in IndexNow notification after generation.
// No notification happens unless a notifier is configured. This example uses a
// local fake server instead of calling a real endpoint.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	sitemap "github.com/umitcekirge/go-sitemap"
)

func main() {
	// Stand-in for the real IndexNow endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	notifier := &sitemap.IndexNowNotifier{
		Key:      "your-indexnow-key",
		Host:     "example.com",
		Endpoint: srv.URL, // omit in production to use the default IndexNow endpoint
		Client:   srv.Client(),
		// Submit the URLs that actually changed, not the sitemap URLs:
		URLs: []string{"https://example.com/p/42", "https://example.com/p/43"},
	}

	gen, err := sitemap.New(sitemap.Options{
		BaseURL:   "https://example.com",
		Notifiers: []sitemap.Notifier{notifier},
	})
	if err != nil {
		log.Fatal(err)
	}

	pages := &sitemap.SliceProvider{
		ProviderName: "pages",
		EntityKind:   sitemap.KindPage,
		Entries:      []sitemap.Entry{{Loc: "https://example.com/p/42"}},
	}
	res, err := gen.Generate(context.Background(), pages)
	if err != nil {
		log.Fatal(err)
	}
	for _, n := range res.Notifications {
		fmt.Printf("notifier %s: err=%v\n", n.Notifier, n.Err)
	}
}

// Command multilingual shows hreflang alternates for a multilingual site.
// Alternates should be reciprocal and complete across all language variants.
package main

import (
	"context"
	"fmt"
	"log"

	sitemap "github.com/umitcekirge/go-sitemap"
)

func main() {
	alts := []sitemap.Alternate{
		{HrefLang: "en", Href: "https://example.com/en/page"},
		{HrefLang: "de", Href: "https://example.com/de/seite"},
		{HrefLang: "fr", Href: "https://example.com/fr/page"},
		{HrefLang: "x-default", Href: "https://example.com/en/page"},
	}

	pages := &sitemap.SliceProvider{
		ProviderName: "pages",
		EntityKind:   sitemap.KindPage,
		Entries: []sitemap.Entry{
			{Loc: "https://example.com/en/page", Alternates: alts},
			{Loc: "https://example.com/de/seite", Alternates: alts},
			{Loc: "https://example.com/fr/page", Alternates: alts},
		},
	}

	store := sitemap.NewMemoryOutput()
	gen, err := sitemap.New(sitemap.Options{BaseURL: "https://example.com", Output: store})
	if err != nil {
		log.Fatal(err)
	}

	res, err := gen.Generate(context.Background(), pages)
	if err != nil {
		log.Fatal(err)
	}
	data, _ := store.Get(res.Files[0].Name)
	fmt.Println(string(data))
}

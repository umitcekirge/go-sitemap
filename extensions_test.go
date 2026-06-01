package sitemap

import (
	"bytes"
	"context"
	"encoding/xml"
	"testing"
	"time"
)

func genOne(t *testing.T, e Entry) []byte {
	t.Helper()
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{e}}
	g, mo := newGen(t, Options{})
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return readFile(t, mo, res.Files[0].Name)
}

func TestHreflangNamespaceOnlyWhenNeeded(t *testing.T) {
	withAlt := genOne(t, Entry{
		Loc: "https://example.com/en",
		Alternates: []Alternate{
			{HrefLang: "en", Href: "https://example.com/en"},
			{HrefLang: "de", Href: "https://example.com/de"},
			{HrefLang: "x-default", Href: "https://example.com/"},
		},
	})
	if !bytes.Contains(withAlt, []byte(`xmlns:xhtml="http://www.w3.org/1999/xhtml"`)) {
		t.Fatalf("xhtml namespace missing when alternates present:\n%s", withAlt)
	}
	if !bytes.Contains(withAlt, []byte(`<xhtml:link rel="alternate" hreflang="x-default" href="https://example.com/"/>`)) {
		t.Fatalf("x-default alternate missing:\n%s", withAlt)
	}

	plain := genOne(t, Entry{Loc: "https://example.com/en"})
	if bytes.Contains(plain, []byte("xmlns:xhtml")) {
		t.Fatalf("xhtml namespace should not appear without alternates:\n%s", plain)
	}
}

func TestImageNamespaceAndFields(t *testing.T) {
	data := genOne(t, Entry{
		Loc: "https://example.com/p",
		Images: []Image{
			{Loc: "https://cdn.example.com/a.jpg", Title: "A & B", Caption: "cap", License: "https://example.com/lic"},
			{Loc: "https://cdn.example.com/b.jpg"},
		},
	})
	if !bytes.Contains(data, []byte(`xmlns:image="http://www.google.com/schemas/sitemap-image/1.1"`)) {
		t.Fatalf("image namespace missing:\n%s", data)
	}
	if !bytes.Contains(data, []byte("<image:loc>https://cdn.example.com/a.jpg</image:loc>")) {
		t.Fatalf("image loc missing:\n%s", data)
	}
	if !bytes.Contains(data, []byte("<image:title>A &amp; B</image:title>")) {
		t.Fatalf("image title not escaped:\n%s", data)
	}
	if bytes.Contains(data, []byte("xmlns:video")) || bytes.Contains(data, []byte("xmlns:news")) {
		t.Fatalf("unexpected video/news namespaces:\n%s", data)
	}
}

func TestVideoNamespaceAndValidation(t *testing.T) {
	pub := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	data := genOne(t, Entry{
		Loc: "https://example.com/v",
		Videos: []Video{{
			ThumbnailLoc:    "https://cdn.example.com/t.jpg",
			Title:           "Title",
			Description:     "Desc",
			ContentLoc:      "https://cdn.example.com/v.mp4",
			Duration:        120,
			PublicationDate: &pub,
		}},
	})
	if !bytes.Contains(data, []byte(`xmlns:video="http://www.google.com/schemas/sitemap-video/1.1"`)) {
		t.Fatalf("video namespace missing:\n%s", data)
	}
	if !bytes.Contains(data, []byte("<video:duration>120</video:duration>")) {
		t.Fatalf("video duration missing:\n%s", data)
	}

	// Missing required fields must fail validation.
	bad := []Video{
		{Title: "t", Description: "d", ContentLoc: "https://e.com/v"},                                                       // no thumbnail
		{ThumbnailLoc: "https://e.com/t.jpg", Description: "d", ContentLoc: "https://e.com/v"},                              // no title
		{ThumbnailLoc: "https://e.com/t.jpg", Title: "t", Description: "d"},                                                 // no content/player
		{ThumbnailLoc: "https://e.com/t.jpg", Title: "t", Description: "d", ContentLoc: "https://e.com/v", Duration: 99999}, // bad duration
	}
	for i, v := range bad {
		if err := validateVideo(&v, "https://e.com/x"); err == nil {
			t.Fatalf("bad video %d should fail validation", i)
		}
	}
}

func TestNewsNamespaceAndValidation(t *testing.T) {
	data := genOne(t, Entry{
		Loc: "https://example.com/article",
		News: &News{
			PublicationName:     "The Example Times",
			PublicationLanguage: "en",
			PublicationDate:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Title:               "Breaking",
		},
	})
	if !bytes.Contains(data, []byte(`xmlns:news="http://www.google.com/schemas/sitemap-news/0.9"`)) {
		t.Fatalf("news namespace missing:\n%s", data)
	}
	if !bytes.Contains(data, []byte("<news:name>The Example Times</news:name>")) {
		t.Fatalf("news name missing:\n%s", data)
	}

	bad := &News{PublicationLanguage: "en", PublicationDate: time.Now(), Title: "t"} // no name
	if err := validateNews(bad, "https://e.com/x"); err == nil {
		t.Fatalf("incomplete news should fail validation")
	}
}

func TestMultipleExtensionsSameEntry(t *testing.T) {
	data := genOne(t, Entry{
		Loc:        "https://example.com/multi",
		Alternates: []Alternate{{HrefLang: "en", Href: "https://example.com/en"}},
		Images:     []Image{{Loc: "https://cdn.example.com/a.jpg"}},
	})
	if !bytes.Contains(data, []byte("xmlns:xhtml")) || !bytes.Contains(data, []byte("xmlns:image")) {
		t.Fatalf("both namespaces should be present:\n%s", data)
	}
	// Must remain valid XML.
	var doc xmlURLSet
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("multi-extension doc must parse: %v", err)
	}
}

func TestPrettyXML(t *testing.T) {
	p := &SliceProvider{ProviderName: "pages", EntityKind: KindPage, Entries: []Entry{{Loc: "https://example.com/p"}}}
	g, mo := newGen(t, Options{PrettyXML: true})
	res, err := g.Generate(context.Background(), p)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data := readFile(t, mo, res.Files[0].Name)
	if !bytes.Contains(data, []byte("\n\t<url>")) {
		t.Fatalf("pretty XML should indent <url>:\n%s", data)
	}
}

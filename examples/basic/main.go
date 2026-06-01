// Command basic generates a sitemap for a few static pages to the local
// filesystem and prints the robots.txt lines to advertise it.
package main

import (
	"context"
	"fmt"
	"log"

	sitemap "github.com/umitcekirge/go-sitemap"
)

func main() {
	pages := &sitemap.SliceProvider{
		ProviderName: "pages",
		EntityKind:   sitemap.KindPage,
		Entries: []sitemap.Entry{
			{Loc: "https://example.com/"},
			{Loc: "https://example.com/about"},
			{Loc: "https://example.com/contact"},
		},
	}

	gen, err := sitemap.New(sitemap.Options{
		BaseURL: "https://example.com",
		Output:  sitemap.NewFileOutput("./public"),
	})
	if err != nil {
		log.Fatal(err)
	}

	res, err := gen.Generate(context.Background(), pages)
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range res.Files {
		fmt.Printf("wrote %s (%d urls, %d bytes) -> %s\n", f.Name, f.URLCount, f.UncompressedSize, f.PublicURL)
	}
	fmt.Print("\nAdd to robots.txt:\n", sitemap.RobotsSitemapLinesFromResult(res))
}

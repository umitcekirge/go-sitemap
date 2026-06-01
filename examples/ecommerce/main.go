// Command ecommerce demonstrates a production-style setup for a large store:
// multiple typed providers, streaming from a paginated source, gzip output,
// custom split limits, automatic index generation, and result stats.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	sitemap "github.com/umitcekirge/go-sitemap"
)

// streamProducts simulates streaming products from a database, page by page,
// so the whole catalogue is never held in memory.
func streamProducts(total int) func(ctx context.Context, yield func(sitemap.Entry) error) error {
	return func(ctx context.Context, yield func(sitemap.Entry) error) error {
		const pageSize = 1000
		for offset := 0; offset < total; offset += pageSize {
			if err := ctx.Err(); err != nil {
				return err
			}
			// In real code: SELECT ... LIMIT pageSize OFFSET offset.
			for i := offset; i < offset+pageSize && i < total; i++ {
				updated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i%30)
				if err := yield(sitemap.Entry{
					Loc:     fmt.Sprintf("https://shop.example.com/p/%d", i),
					LastMod: &updated, // only set on a meaningful change
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

func main() {
	products := &sitemap.FuncProvider{
		ProviderName: "products",
		EntityKind:   sitemap.KindProduct,
		StreamFunc:   streamProducts(120000),
	}
	categories := &sitemap.SliceProvider{
		ProviderName: "categories",
		EntityKind:   sitemap.KindCategory,
		Entries: []sitemap.Entry{
			{Loc: "https://shop.example.com/c/shoes"},
			{Loc: "https://shop.example.com/c/shirts"},
		},
	}

	gen, err := sitemap.New(sitemap.Options{
		BaseURL:           "https://shop.example.com",
		Gzip:              true,
		MaxURLsPerSitemap: 50000, // protocol default; lower it for debugging
	})
	if err != nil {
		log.Fatal(err)
	}

	res, err := gen.Generate(context.Background(), products, categories)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("total=%d written=%d files=%d index=%d duration=%s\n",
		res.TotalURLs, res.WrittenURLs, len(res.Files), len(res.IndexFiles), res.Duration)
	for _, ps := range res.Providers {
		fmt.Printf("  %-12s kind=%-8s files=%d urls=%d bytes=%d\n",
			ps.Name, ps.Kind, ps.FileCount, ps.WrittenURLs, ps.UncompressedBytes)
	}
}

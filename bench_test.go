package sitemap

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// discardOutput throws away bytes so benchmarks isolate generation cost from
// output storage. It does not retain the slice.
type discardOutput struct{ bytes int64 }

func (d *discardOutput) Write(ctx context.Context, name string, data []byte) error {
	d.bytes += int64(len(data))
	return nil
}

// benchProvider streams n simple entries without allocating a backing slice.
func benchProvider(name string, n int, decorate func(i int, e *Entry)) *FuncProvider {
	return &FuncProvider{
		ProviderName: name, EntityKind: KindProduct,
		StreamFunc: func(ctx context.Context, yield func(Entry) error) error {
			for i := range n {
				e := Entry{Loc: "https://example.com/products/item-" + strconv.Itoa(i)}
				if decorate != nil {
					decorate(i, &e)
				}
				if err := yield(e); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func benchRun(b *testing.B, opts Options, n int, decorate func(int, *Entry)) {
	b.Helper()
	opts.BaseURL = "https://example.com"
	opts.Output = &discardOutput{}
	opts.Clock = fixedClock()
	g, err := New(opts)
	if err != nil {
		b.Fatal(err)
	}
	p := benchProvider("products", n, decorate)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := g.Generate(context.Background(), p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlain100k(b *testing.B) {
	benchRun(b, Options{}, 100_000, nil)
}

func BenchmarkGzip100k(b *testing.B) {
	benchRun(b, Options{Gzip: true}, 100_000, nil)
}

func BenchmarkExtensions50k(b *testing.B) {
	lm := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	benchRun(b, Options{}, 50_000, func(i int, e *Entry) {
		e.LastMod = &lm
		e.Images = []Image{{Loc: "https://cdn.example.com/img-" + strconv.Itoa(i) + ".jpg", Title: "Item & Co."}}
		e.Alternates = []Alternate{
			{HrefLang: "en", Href: "https://example.com/en/item-" + strconv.Itoa(i)},
			{HrefLang: "de", Href: "https://example.com/de/item-" + strconv.Itoa(i)},
		}
	})
}

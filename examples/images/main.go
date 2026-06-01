// Command images demonstrates the image sitemap extension. Namespaces are
// emitted only on files that actually contain image data.
package main

import (
	"context"
	"fmt"
	"log"

	sitemap "github.com/umitcekirge/go-sitemap"
)

func main() {
	gallery := &sitemap.SliceProvider{
		ProviderName: "gallery",
		EntityKind:   sitemap.KindImage,
		Entries: []sitemap.Entry{
			{
				Loc: "https://example.com/gallery/sunset",
				Images: []sitemap.Image{
					{Loc: "https://cdn.example.com/sunset-1.jpg", Title: "Sunset over the bay", Caption: "Golden hour"},
					{Loc: "https://cdn.example.com/sunset-2.jpg", License: "https://example.com/license"},
				},
			},
		},
	}

	store := sitemap.NewMemoryOutput()
	gen, err := sitemap.New(sitemap.Options{BaseURL: "https://example.com", Output: store, PrettyXML: true})
	if err != nil {
		log.Fatal(err)
	}
	res, err := gen.Generate(context.Background(), gallery)
	if err != nil {
		log.Fatal(err)
	}
	data, _ := store.Get(res.Files[0].Name)
	fmt.Println(string(data))
}

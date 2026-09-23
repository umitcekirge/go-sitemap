package main

import (
	"testing"
	"time"
)

func TestPostURL(t *testing.T) {
	pages := hierarchy{
		1: {0, "about"},
		2: {1, "team"},
	}
	date := time.Date(2024, 3, 7, 9, 5, 0, 0, time.UTC)
	post := postRow{id: 42, name: "hello", date: date, author: "jo"}

	tests := []struct {
		name, structure, postType string
		p                         postRow
		want                      string
	}{
		{"plain post", "", "post", post, "https://x.test/?p=42"},
		{"postname", "/%postname%/", "post", post, "https://x.test/hello/"},
		{"date", "/%year%/%monthnum%/%day%/%postname%/", "post", post, "https://x.test/2024/03/07/hello/"},
		{"no trailing slash", "/%post_id%-%postname%", "post", post, "https://x.test/42-hello"},
		{"child page", "/%postname%/", "page", postRow{id: 2}, "https://x.test/about/team/"},
		{"pathinfo page", "/index.php/%postname%/", "page", postRow{id: 1}, "https://x.test/index.php/about/"},
		{"front page", "/%postname%/", "page", postRow{id: 7}, "https://x.test/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &site{home: "https://x.test", structure: tt.structure, frontPageID: 7}
			if got := s.postURL(tt.postType, tt.p, pages.path); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTermAndAuthorURL(t *testing.T) {
	s := &site{home: "https://x.test", structure: "/blog/%postname%/", catBase: "topics"}
	if got := s.termURL("category", "news/local", "local", 5); got != "https://x.test/topics/news/local/" {
		t.Fatalf("category url = %q", got)
	}
	if got := s.termURL("post_tag", "go", "go", 9); got != "https://x.test/blog/tag/go/" {
		t.Fatalf("tag url = %q", got)
	}
	if got := s.authorURL(3, "jo"); got != "https://x.test/blog/author/jo/" {
		t.Fatalf("author url = %q", got)
	}
}

func TestHierarchyCycleTerminates(t *testing.T) {
	h := hierarchy{1: {2, "a"}, 2: {1, "b"}}
	if got := h.path(1); got == "" {
		t.Fatalf("cycle should still yield a path")
	}
}

package sitemap

import "time"

// Kind identifies the entity type a provider produces. It is informational
// (surfaced in stats) and is also used to derive the default file prefix when a
// provider does not specify one.
type Kind string

// Well-known entity kinds. Custom kinds are allowed: any non-empty string is
// accepted, so applications may define their own.
const (
	KindPage     Kind = "page"
	KindProduct  Kind = "product"
	KindCategory Kind = "category"
	KindBrand    Kind = "brand"
	KindArticle  Kind = "article"
	KindBlogPost Kind = "blog_post"
	KindNews     Kind = "news"
	KindImage    Kind = "image"
	KindVideo    Kind = "video"
	KindCustom   Kind = "custom"
)

// ChangeFreq is the optional <changefreq> value. It is part of the sitemap
// protocol but ignored by modern Google indexing; this package never sets it
// implicitly.
type ChangeFreq string

// Allowed changefreq values per the sitemap protocol.
const (
	Always  ChangeFreq = "always"
	Hourly  ChangeFreq = "hourly"
	Daily   ChangeFreq = "daily"
	Weekly  ChangeFreq = "weekly"
	Monthly ChangeFreq = "monthly"
	Yearly  ChangeFreq = "yearly"
	Never   ChangeFreq = "never"
)

// validChangeFreqs is the set of allowed changefreq values.
var validChangeFreqs = map[ChangeFreq]bool{
	Always: true, Hourly: true, Daily: true, Weekly: true,
	Monthly: true, Yearly: true, Never: true,
}

// Entry is a single <url> element in a sitemap.
//
// Only Loc is required. Optional scalar fields use pointer/zero semantics so the
// generator can distinguish "not set" from a real value and avoid writing fake
// defaults:
//
//   - LastMod: written only when non-nil. Never auto-populated.
//   - Priority: written only when non-nil. Must be within [0.0, 1.0].
//   - ChangeFreq: written only when non-empty. Must be an allowed value.
type Entry struct {
	// Loc is the absolute http(s) URL of the page. Required.
	Loc string
	// LastMod is the time of the last meaningful content change. Optional.
	// It is formatted using Options.LastModFormat (default RFC3339). The
	// generator never sets this to "now".
	LastMod *time.Time
	// ChangeFreq is an optional change-frequency hint.
	ChangeFreq ChangeFreq
	// Priority is an optional relative priority in [0.0, 1.0].
	Priority *float64
	// Alternates are hreflang alternate links (XHTML namespace). Optional.
	Alternates []Alternate
	// Images are image-sitemap extension entries. Optional.
	Images []Image
	// Videos are video-sitemap extension entries. Optional.
	Videos []Video
	// News is a news-sitemap extension entry. Optional.
	News *News
}

// Alternate is one hreflang alternate link for multilingual sites. Both fields
// are required; Href must be an absolute URL. Use HrefLang "x-default" for the
// default fallback. Alternates are emitted in the order provided.
type Alternate struct {
	HrefLang string
	Href     string
}

// Image is an image-sitemap extension entry. Loc is required and must be
// absolute. Title, Caption and License are optional.
type Image struct {
	Loc     string
	Title   string
	Caption string
	License string
}

// Video is a video-sitemap extension entry. ThumbnailLoc, Title and
// Description are required, and at least one of ContentLoc or PlayerLoc must be
// set. All URLs must be absolute.
type Video struct {
	ThumbnailLoc    string
	Title           string
	Description     string
	ContentLoc      string
	PlayerLoc       string
	Duration        int // seconds, optional; if set must be 1..28800
	PublicationDate *time.Time
}

// News is a news-sitemap extension entry. All listed fields are required. News
// sitemaps are only for content eligible for Google News.
type News struct {
	PublicationName     string
	PublicationLanguage string
	PublicationDate     time.Time
	Title               string
}

// hasDuration reports whether the optional duration was supplied.
func (v Video) hasDuration() bool { return v.Duration != 0 }

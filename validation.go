package sitemap

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// hostOf parses an absolute http(s) URL without allocating (unlike net/url) and
// returns its host (authority with any userinfo stripped, e.g. "example.com" or
// "example.com:8443"). It enforces exactly the rules sitemaps require: a non-
// empty http/https scheme and a non-empty host, with no spaces or control
// characters anywhere. It returns a wrapped ErrInvalidURL on failure.
func hostOf(s string) (string, error) {
	if s == "" {
		return "", newErr(ErrInvalidURL, "validate", "url is empty")
	}
	if !utf8.ValidString(s) {
		return "", newErr(ErrInvalidURL, "validate", "url is not valid UTF-8")
	}
	var rest string
	switch {
	case strings.HasPrefix(s, "https://"):
		rest = s[len("https://"):]
	case strings.HasPrefix(s, "http://"):
		rest = s[len("http://"):]
	default:
		return "", newErr(ErrInvalidURL, "validate", "url scheme is not http or https")
	}
	// The authority runs up to the first '/', '?' or '#'.
	authority := rest
	for i := 0; i < len(rest); i++ {
		if c := rest[i]; c == '/' || c == '?' || c == '#' {
			authority = rest[:i]
			break
		}
	}
	host := authority
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		host = authority[at+1:]
	}
	if host == "" {
		return "", newErr(ErrInvalidURL, "validate", "url has no host")
	}
	// Spaces and control characters are not allowed in a URL and would break
	// the generated XML; reject them rather than silently escaping.
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' {
			return "", newErr(ErrInvalidURL, "validate", "url contains spaces or control characters")
		}
	}
	return host, nil
}

// validateAbsoluteURL reports whether s is a non-empty, absolute http(s) URL
// with a host. It returns a wrapped ErrInvalidURL on failure.
func validateAbsoluteURL(s string) error {
	_, err := hostOf(s)
	return err
}

// validateEntry validates a single entry against protocol and extension rules.
// host, when non-empty, enforces same-host policy on Loc. The returned error,
// when non-nil, wraps ErrInvalidEntry (or ErrInvalidURL) and carries field
// context.
func validateEntry(e Entry, host string) error {
	locHost, err := hostOf(e.Loc)
	if err != nil {
		var se *Error
		if errors.As(err, &se) {
			se.Field = "Loc"
		}
		return err
	}
	if host != "" && !strings.EqualFold(locHost, host) {
		return locErr(ErrInvalidEntry, "Loc", e.Loc,
			fmt.Sprintf("host %q does not match required host %q", locHost, host))
	}

	if e.Priority != nil {
		if p := *e.Priority; math.IsNaN(p) || p < 0 || p > 1 {
			return locErr(ErrInvalidEntry, "Priority", e.Loc,
				fmt.Sprintf("priority %v is outside [0.0, 1.0]", p))
		}
	}
	if e.ChangeFreq != "" && !validChangeFreqs[e.ChangeFreq] {
		return locErr(ErrInvalidEntry, "ChangeFreq", e.Loc,
			fmt.Sprintf("changefreq %q is not an allowed value", e.ChangeFreq))
	}

	for i := range e.Alternates {
		a := e.Alternates[i]
		if a.HrefLang == "" {
			return locErr(ErrInvalidEntry, "Alternate.HrefLang", e.Loc, "hreflang is empty")
		}
		if err := validText("Alternate.HrefLang", e.Loc, a.HrefLang); err != nil {
			return err
		}
		if err := validateAbsoluteURL(a.Href); err != nil {
			return locErr(ErrInvalidEntry, "Alternate.Href", e.Loc, "alternate href must be absolute")
		}
	}

	for i := range e.Images {
		im := &e.Images[i]
		if err := validateAbsoluteURL(im.Loc); err != nil {
			return locErr(ErrInvalidEntry, "Image.Loc", e.Loc, "image loc must be absolute")
		}
		if err := validTexts(e.Loc, "Image.Title", im.Title, "Image.Caption", im.Caption, "Image.License", im.License); err != nil {
			return err
		}
	}

	for i := range e.Videos {
		if err := validateVideo(&e.Videos[i], e.Loc); err != nil {
			return err
		}
	}

	if e.News != nil {
		if err := validateNews(e.News, e.Loc); err != nil {
			return err
		}
	}

	return nil
}

// validateVideo checks required video fields.
func validateVideo(v *Video, loc string) error {
	if err := validateAbsoluteURL(v.ThumbnailLoc); err != nil {
		return locErr(ErrInvalidEntry, "Video.ThumbnailLoc", loc, "video thumbnail_loc must be absolute")
	}
	if strings.TrimSpace(v.Title) == "" {
		return locErr(ErrInvalidEntry, "Video.Title", loc, "video title is required")
	}
	if strings.TrimSpace(v.Description) == "" {
		return locErr(ErrInvalidEntry, "Video.Description", loc, "video description is required")
	}
	if v.ContentLoc == "" && v.PlayerLoc == "" {
		return locErr(ErrInvalidEntry, "Video.ContentLoc", loc, "video requires content_loc or player_loc")
	}
	if v.ContentLoc != "" {
		if err := validateAbsoluteURL(v.ContentLoc); err != nil {
			return locErr(ErrInvalidEntry, "Video.ContentLoc", loc, "video content_loc must be absolute")
		}
	}
	if v.PlayerLoc != "" {
		if err := validateAbsoluteURL(v.PlayerLoc); err != nil {
			return locErr(ErrInvalidEntry, "Video.PlayerLoc", loc, "video player_loc must be absolute")
		}
	}
	if v.hasDuration() && (v.Duration < 1 || v.Duration > 28800) {
		return locErr(ErrInvalidEntry, "Video.Duration", loc, "video duration must be within 1..28800 seconds")
	}
	return validTexts(loc, "Video.Title", v.Title, "Video.Description", v.Description)
}

// validateNews checks required news fields.
func validateNews(n *News, loc string) error {
	if strings.TrimSpace(n.PublicationName) == "" {
		return locErr(ErrInvalidEntry, "News.PublicationName", loc, "news publication name is required")
	}
	if strings.TrimSpace(n.PublicationLanguage) == "" {
		return locErr(ErrInvalidEntry, "News.PublicationLanguage", loc, "news publication language is required")
	}
	if n.PublicationDate.IsZero() {
		return locErr(ErrInvalidEntry, "News.PublicationDate", loc, "news publication date is required")
	}
	if strings.TrimSpace(n.Title) == "" {
		return locErr(ErrInvalidEntry, "News.Title", loc, "news title is required")
	}
	return validTexts(loc, "News.PublicationName", n.PublicationName,
		"News.PublicationLanguage", n.PublicationLanguage, "News.Title", n.Title)
}

// validText rejects invalid UTF-8, which escape would otherwise copy into the
// output and make the whole file unparseable.
func validText(field, loc, s string) error {
	if !utf8.ValidString(s) {
		return locErr(ErrInvalidEntry, field, loc, "value is not valid UTF-8")
	}
	return nil
}

// validTexts applies validText to alternating field/value pairs.
func validTexts(loc string, pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := validText(pairs[i], loc, pairs[i+1]); err != nil {
			return err
		}
	}
	return nil
}

// locErr builds an *Error with field and loc context.
func locErr(kind error, field, loc, msg string) *Error {
	return &Error{Op: "validate", Field: field, Loc: loc, Msg: msg, kind: kind}
}

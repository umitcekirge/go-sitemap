package sitemap

import (
	"errors"
	"fmt"
)

// Sentinel errors. Use errors.Is to test for a category and errors.As to obtain
// the structured *Error (and its Field/Loc context) where one is returned.
var (
	// ErrInvalidOptions indicates the Options passed to New were invalid.
	ErrInvalidOptions = errors.New("sitemap: invalid options")
	// ErrInvalidProvider indicates a provider was nil or had an invalid name.
	ErrInvalidProvider = errors.New("sitemap: invalid provider")
	// ErrInvalidURL indicates a URL was not an absolute http(s) URL.
	ErrInvalidURL = errors.New("sitemap: invalid url")
	// ErrInvalidEntry indicates an entry failed validation.
	ErrInvalidEntry = errors.New("sitemap: invalid entry")
	// ErrEntryTooLarge indicates a single entry cannot fit within the
	// configured per-file uncompressed byte limit.
	ErrEntryTooLarge = errors.New("sitemap: entry too large for a single sitemap file")
	// ErrProvider indicates a provider's Stream method returned an error.
	ErrProvider = errors.New("sitemap: provider failure")
	// ErrOutput indicates the output backend failed to write.
	ErrOutput = errors.New("sitemap: output failure")
	// ErrXML indicates an XML writing failure.
	ErrXML = errors.New("sitemap: xml writing failure")
	// ErrGzip indicates a gzip failure.
	ErrGzip = errors.New("sitemap: gzip failure")
	// ErrIndex indicates a sitemap index generation failure.
	ErrIndex = errors.New("sitemap: index generation failure")
	// ErrNotify indicates a notification failure.
	ErrNotify = errors.New("sitemap: notification failure")
)

// Error is a structured error carrying the offending field/value context. It
// wraps one of the sentinel errors above so errors.Is keeps working.
type Error struct {
	// Op is a short operation tag, e.g. "validate", "write", "index".
	Op string
	// Provider is the provider name when relevant.
	Provider string
	// Field is the offending field name when relevant, e.g. "Loc", "Priority".
	Field string
	// Loc is the entry location when relevant.
	Loc string
	// Msg is a human-readable explanation.
	Msg string
	// kind is the wrapped sentinel error used by errors.Is.
	kind error
	// cause is an optional underlying error used by errors.Unwrap.
	cause error
}

func (e *Error) Error() string {
	var b []byte
	if e.Op != "" {
		b = append(b, e.Op...)
		b = append(b, ": "...)
	}
	if e.Provider != "" {
		b = append(b, fmt.Sprintf("provider %q: ", e.Provider)...)
	}
	b = append(b, e.Msg...)
	if e.Field != "" {
		b = append(b, fmt.Sprintf(" (field %s)", e.Field)...)
	}
	if e.Loc != "" {
		b = append(b, fmt.Sprintf(" (loc %s)", e.Loc)...)
	}
	if e.cause != nil {
		b = append(b, ": "...)
		b = append(b, e.cause.Error()...)
	}
	return string(b)
}

// Is reports whether the error matches the wrapped sentinel.
func (e *Error) Is(target error) bool { return target == e.kind }

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.cause }

// newErr builds an *Error wrapping the given sentinel kind.
func newErr(kind error, op, msg string) *Error {
	return &Error{Op: op, Msg: msg, kind: kind}
}

// wrap attaches an underlying cause to an *Error.
func (e *Error) wrap(cause error) *Error {
	e.cause = cause
	return e
}

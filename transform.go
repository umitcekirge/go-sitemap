package sitemap

import "context"

// applyTransforms runs every configured transform on e in order. It returns
// keep=false as soon as a transform drops the entry, and an error (wrapping the
// transform's error) if one fails. Transforms may mutate *e in place.
func applyTransforms(ctx context.Context, transforms []Transform, e *Entry) (bool, error) {
	for _, t := range transforms {
		if t == nil {
			continue
		}
		keep, err := t(ctx, e)
		if err != nil {
			return false, newErr(ErrInvalidEntry, "transform", "transform returned an error").wrap(err)
		}
		if !keep {
			return false, nil
		}
	}
	return true, nil
}

// TransformChain composes multiple transforms into one. It is a convenience for
// building a single Transform from a list; entries are dropped as soon as any
// transform returns keep=false.
func TransformChain(transforms ...Transform) Transform {
	return func(ctx context.Context, e *Entry) (bool, error) {
		return applyTransforms(ctx, transforms, e)
	}
}

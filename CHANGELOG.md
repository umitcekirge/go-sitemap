# Changelog

All notable changes to this project are documented here. The project follows
[Semantic Versioning](https://semver.org/); before v1.0.0, minor versions may
contain breaking changes.

## v0.1.0 — 2026-09-23

First tagged release.

### Features

- Provider-based generation with streaming and bounded memory.
- Automatic splitting by URL count (50,000) and uncompressed size (50 MB), with
  a sitemap index split by the same limits.
- Gzip output, deterministic file naming, strict/lenient/collect validation.
- Extensions: hreflang, image, video and news.
- `FileOutput` (atomic writes), `MemoryOutput`, and an optional `FileServer`.
- All-or-nothing publishing: a failed run leaves the published files
  untouched (`Stager`).
- Stale-file cleanup after a successful run, on by default (`Pruner`,
  `KeepStaleFiles`).
- Opt-in notifiers: IndexNow and a legacy ping shim.
- Examples, including a WordPress generator in a separate module.

### Breaking changes since the untagged initial import

- `Options.Output` is required unless `DryRun` is set; it no longer falls back
  to an in-memory store.
- `Options.DisableIndex` is removed; a multi-file set always gets an index.
- `ErrValidation` is removed; it was never returned.
- `IndexNowNotifier` no longer submits sitemap URLs when `URLs` is empty.
- Priority is written at full precision (`0.85`, `1`) instead of one decimal.

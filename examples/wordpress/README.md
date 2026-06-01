# WordPress sitemap generator (example)

Generates a WordPress sitemap set with [`go-sitemap`](../../), reading content
directly from the WordPress **MySQL database**. It reproduces the file layout of
WordPress core sitemaps (since WP 5.5):

```
wp-sitemap.xml                        ← index
wp-sitemap-posts-post-1.xml           ← posts
wp-sitemap-posts-page-1.xml           ← pages
wp-sitemap-taxonomies-category-1.xml  ← categories
wp-sitemap-taxonomies-post_tag-1.xml  ← tags (if any)
wp-sitemap-users-1.xml                ← authors
```

This is a **separate Go module** so the core `go-sitemap` package stays
dependency-free; only this example depends on the MySQL driver.

## What it does

- Connects to MySQL with the credentials you pass on the command line (or a full
  `-dsn`); optionally reads them from a `wp-config.php` instead.
- Reads the site URL (`home`) and `permalink_structure` from `wp_options`, so
  it generates correct plain (`?p=123`) or pretty (`/slug/`) URLs automatically.
- Streams content from the database with **keyset pagination** (`WHERE id > ?
  ORDER BY id LIMIT n`), so it scales to very large sites with bounded memory.
- One provider per content group: each public post type, each taxonomy, and
  authors — mirroring WordPress core, but using a WordPress-style `FileNamer`.
- Includes a `<lastmod>` from `post_modified_gmt` (a meaningful content-change
  time) — something WordPress core omits, and a small SEO win.
- Excludes drafts/auto-drafts, password-protected and trashed content, and
  empty taxonomy terms; only `publish` status is included.

## Run

With your MySQL credentials (the usual way):

```sh
cd examples/wordpress
go run . -host 127.0.0.1:3306 -user root -password root -database wordpress7 -out ./wp-sitemaps
```

Or with a full DSN:

```sh
go run . -dsn 'root:root@tcp(127.0.0.1:3306)/wordpress7' -out ./wp-sitemaps
```

Or let it read credentials from a `wp-config.php` (optional convenience):

```sh
go run . -wp-config /path/to/wordpress/wp-config.php -out ./wp-sitemaps
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-host` | `127.0.0.1:3306` | MySQL host[:port] |
| `-user` | `root` | MySQL user |
| `-password` | (or `WP_DB_PASSWORD` env) | MySQL password |
| `-database` | — | WordPress database name (required unless `-dsn`/`-wp-config`) |
| `-prefix` | `wp_` | table prefix |
| `-dsn` | — | full MySQL DSN (overrides the connection flags) |
| `-wp-config` | — | optional: read credentials + prefix from a `wp-config.php` |
| `-out` | `./wp-sitemaps` | output directory |
| `-gzip` | `false` | write `.xml.gz` |
| `-per-file` | `2000` | max URLs per file (WordPress core uses 2000) |
| `-base` | (from DB) | override the site base URL |

The password can be supplied via the `WP_DB_PASSWORD` environment variable to
keep it off the command line.

To serve the result like WordPress does, copy the files to your web root so
`https://your-site/wp-sitemap.xml` resolves, and add the printed `Sitemap:` line
to `robots.txt`.

## Public post types

WordPress decides which post types are "public" in PHP at registration time;
that flag is **not stored in the database**. This example therefore uses the
standard allowlist (`post`, `page`). To publish a custom post type, add it to
the `postTypes` slice in `main.go`. Internal types such as `wp_global_styles`,
`wp_navigation` and `wp_template` are excluded by virtue of the allowlist.

## Permalink notes

- **Plain** permalinks (`permalink_structure` empty) produce query URLs:
  `?p=ID`, `?page_id=ID`, `?cat=term_id`, `?tag=slug`, `?author=ID`.
- **Pretty** permalinks are handled for the common `%postname%` style
  (`/slug/`, `/category/slug/`, `/tag/slug/`, `/author/nicename/`). Date-based
  structures (`/%year%/%monthnum%/%postname%/`) would need the post date woven
  into the path; extend `site.postURL` if you use those.

## How it maps to the core package

- `FuncProvider` + keyset pagination → streaming, bounded memory.
- `FilePrefix` per provider + a custom `FileNamer` → exact WordPress file names.
- `AlwaysIndex` + `IndexBaseName: "wp-sitemap"` → the `wp-sitemap.xml` index.
- `Entry.LastMod` set only from real modification times → honest `lastmod`.

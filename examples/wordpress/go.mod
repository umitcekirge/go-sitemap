module github.com/umitcekirge/go-sitemap/examples/wordpress

go 1.24.0

toolchain go1.24.2

require (
	github.com/go-sql-driver/mysql v1.10.0
	github.com/umitcekirge/go-sitemap v0.0.0
)

require filippo.io/edwards25519 v1.2.0 // indirect

// Use the package from this repository rather than a published version.
replace github.com/umitcekirge/go-sitemap => ../..

// Command wordpress generates a WordPress sitemap set with go-sitemap, reading
// content directly from the WordPress MySQL database.
//
// It mirrors the structure of WordPress core sitemaps (since WP 5.5):
//
//	wp-sitemap.xml                          (index)
//	wp-sitemap-posts-post-1.xml             (posts)
//	wp-sitemap-posts-page-1.xml             (pages)
//	wp-sitemap-taxonomies-category-1.xml    (categories)
//	wp-sitemap-users-1.xml                  (authors)
//
// Unlike WordPress core, each entry also carries a <lastmod> derived from
// post_modified_gmt (a meaningful content-change time), which is good for SEO.
//
// Connect with your MySQL credentials directly:
//
//	go run . -host 127.0.0.1:3306 -user root -password root -database wordpress7 -out ./wp-sitemaps
//
// Or pass a full DSN:
//
//	go run . -dsn 'user:pass@tcp(127.0.0.1:3306)/wordpress7' -out ./wp-sitemaps
//
// As a convenience you may instead point at an install's wp-config.php to read
// the credentials and table prefix from it:
//
//	go run . -wp-config /path/to/wordpress/wp-config.php -out ./wp-sitemaps
//
// The site URL and permalink structure are always read from wp_options, so URLs
// are correct without any extra configuration. The password may also be supplied
// via the WP_DB_PASSWORD environment variable to keep it off the command line.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	sitemap "github.com/umitcekirge/go-sitemap"
)

func main() {
	var (
		dsn      = flag.String("dsn", "", "full MySQL DSN (overrides -host/-user/-password/-database)")
		host     = flag.String("host", "127.0.0.1:3306", "MySQL host[:port]")
		user     = flag.String("user", "root", "MySQL user")
		password = flag.String("password", "", "MySQL password (or set WP_DB_PASSWORD)")
		database = flag.String("database", "", "WordPress database name")
		prefix   = flag.String("prefix", "wp_", "WordPress table prefix")
		wpConfig = flag.String("wp-config", "", "optional: read DB credentials and prefix from this wp-config.php")
		outDir   = flag.String("out", "./wp-sitemaps", "output directory")
		gzip     = flag.Bool("gzip", false, "gzip-compress output (.xml.gz)")
		perFile  = flag.Int("per-file", 2000, "max URLs per sitemap file (WordPress core uses 2000)")
		baseURL  = flag.String("base", "", "override the site base URL (default: WordPress 'home' option)")
	)
	flag.Parse()

	if *password == "" {
		*password = os.Getenv("WP_DB_PASSWORD")
	}

	dataSource, tablePrefix, err := resolveDB(*dsn, *wpConfig, *host, *user, *password, *database, *prefix)
	if err != nil {
		flag.Usage()
		log.Fatal(err)
	}

	if err := run(dataSource, tablePrefix, *outDir, *baseURL, *perFile, *gzip); err != nil {
		log.Fatal(err)
	}
}

// resolveDB determines the MySQL DSN and table prefix from, in priority order:
// an explicit -dsn, a -wp-config file, or the individual connection flags.
func resolveDB(dsn, wpConfig, host, user, password, database, prefix string) (string, string, error) {
	switch {
	case dsn != "":
		if !reSafe.MatchString(prefix) {
			return "", "", fmt.Errorf("unsafe table prefix %q", prefix)
		}
		return dsn, prefix, nil
	case wpConfig != "":
		cfg, err := parseWPConfig(wpConfig)
		if err != nil {
			return "", "", fmt.Errorf("read wp-config: %w", err)
		}
		return cfg.dsn(), cfg.prefix, nil
	default:
		if database == "" {
			return "", "", fmt.Errorf("provide -database (and -user/-password/-host), or -dsn, or -wp-config")
		}
		cfg := dbConfig{user: user, pass: password, host: host, name: database, prefix: prefix}
		if !reSafe.MatchString(cfg.prefix) {
			return "", "", fmt.Errorf("unsafe table prefix %q", prefix)
		}
		return cfg.dsn(), cfg.prefix, nil
	}
}

func run(dataSource, tablePrefix, outDir, baseOverride string, perFile int, gzip bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := sql.Open("mysql", dataSource)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to db: %w", err)
	}

	s, err := loadSite(ctx, db, tablePrefix)
	if err != nil {
		return fmt.Errorf("load site settings: %w", err)
	}
	if baseOverride != "" {
		s.home = strings.TrimRight(baseOverride, "/")
	}
	log.Printf("site: %s (permalinks: %s)", s.home, permalinkLabel(s.structure))

	// Public post types to publish. WordPress decides "public" at registration
	// time in PHP (not stored in the DB), so we use the standard allowlist; add
	// your custom post types here.
	postTypes := []struct{ prefix, postType, kind string }{
		{"posts-post", "post", "post"},
		{"posts-page", "page", "page"},
	}

	var providers []sitemap.Provider
	for _, pt := range postTypes {
		providers = append(providers, postTypeProvider(db, s, pt.prefix, pt.postType, pt.kind))
	}
	providers = append(providers,
		termProvider(db, s, "taxonomies-category", "category"),
		termProvider(db, s, "taxonomies-post_tag", "post_tag"),
		authorProvider(db, s),
	)

	gen, err := sitemap.New(sitemap.Options{
		BaseURL:           s.home,
		Output:            sitemap.NewFileOutput(outDir),
		Gzip:              gzip,
		MaxURLsPerSitemap: perFile,
		AlwaysIndex:       true,         // WordPress always exposes wp-sitemap.xml
		IndexBaseName:     "wp-sitemap", // index file name
		FileNamer:         wpNamer{},    // WordPress-style file names
		// post_modified_gmt is a meaningful change time, so allow lastmod.
	})
	if err != nil {
		return err
	}

	res, err := gen.Generate(ctx, providers...)
	if err != nil {
		return err
	}

	fmt.Printf("\nGenerated %d sitemap file(s) + %d index in %s:\n", len(res.Files), len(res.IndexFiles), outDir)
	for _, f := range append(res.IndexFiles, res.Files...) {
		fmt.Printf("  %-36s %5d urls  %6d bytes  %s\n", f.Name, f.URLCount, f.UncompressedSize, f.PublicURL)
	}
	for _, ps := range res.Providers {
		if ps.WrittenURLs == 0 && ps.SkippedURLs == 0 {
			continue
		}
		fmt.Printf("  provider %-22s kind=%-10s urls=%d skipped=%d\n", ps.Name, ps.Kind, ps.WrittenURLs, ps.SkippedURLs)
	}
	fmt.Print("\nrobots.txt:\n", sitemap.RobotsSitemapLinesFromResult(res))
	return nil
}

// --- WordPress site model -------------------------------------------------

type site struct {
	home      string // no trailing slash
	structure string // permalink_structure ("" => plain query URLs)
	prefix    string // table prefix, e.g. "wp_"
}

func loadSite(ctx context.Context, db *sql.DB, prefix string) (*site, error) {
	rows, err := db.QueryContext(ctx,
		fmt.Sprintf("SELECT option_name, option_value FROM %soptions WHERE option_name IN ('home','siteurl','permalink_structure')", prefix))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s := &site{prefix: prefix}
	var siteurl string
	for rows.Next() {
		var name, val string
		if err := rows.Scan(&name, &val); err != nil {
			return nil, err
		}
		switch name {
		case "home":
			s.home = strings.TrimRight(val, "/")
		case "siteurl":
			siteurl = strings.TrimRight(val, "/")
		case "permalink_structure":
			s.structure = val
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if s.home == "" {
		s.home = siteurl
	}
	if s.home == "" {
		return nil, fmt.Errorf("could not determine site home URL")
	}
	return s, nil
}

// pretty reports whether the install uses pretty permalinks.
func (s *site) pretty() bool { return s.structure != "" }

// postURL builds the permalink for a post/page. For pretty permalinks it
// assumes a %postname% style structure (the most common); date-based structures
// would need the post date woven in.
func (s *site) postURL(postType, name string, id int64) string {
	if s.pretty() {
		return s.home + "/" + name + "/"
	}
	switch postType {
	case "page":
		return fmt.Sprintf("%s/?page_id=%d", s.home, id)
	case "post":
		return fmt.Sprintf("%s/?p=%d", s.home, id)
	default:
		return fmt.Sprintf("%s/?post_type=%s&p=%d", s.home, postType, id)
	}
}

func (s *site) termURL(taxonomy, slug string, termID int64) string {
	if s.pretty() {
		switch taxonomy {
		case "category":
			return s.home + "/category/" + slug + "/"
		case "post_tag":
			return s.home + "/tag/" + slug + "/"
		default:
			return s.home + "/" + taxonomy + "/" + slug + "/"
		}
	}
	switch taxonomy {
	case "category":
		return fmt.Sprintf("%s/?cat=%d", s.home, termID)
	case "post_tag":
		return fmt.Sprintf("%s/?tag=%s", s.home, slug)
	default:
		return fmt.Sprintf("%s/?taxonomy=%s&term_id=%d", s.home, taxonomy, termID)
	}
}

func (s *site) authorURL(id int64, nicename string) string {
	if s.pretty() {
		return s.home + "/author/" + nicename + "/"
	}
	return fmt.Sprintf("%s/?author=%d", s.home, id)
}

// --- Providers (keyset-paginated streaming) -------------------------------

func postTypeProvider(db *sql.DB, s *site, prefix, postType, kind string) sitemap.Provider {
	query := fmt.Sprintf(
		"SELECT ID, post_name, post_modified_gmt FROM %sposts "+
			"WHERE post_type=? AND post_status='publish' AND post_password='' AND ID>? "+
			"ORDER BY ID ASC LIMIT ?", s.prefix)
	return &sitemap.FuncProvider{
		ProviderName: prefix,
		FilePrefix:   prefix,
		EntityKind:   sitemap.Kind(kind),
		StreamFunc: func(ctx context.Context, yield func(sitemap.Entry) error) error {
			const batch = 1000
			var lastID int64
			for {
				rows, err := db.QueryContext(ctx, query, postType, lastID, batch)
				if err != nil {
					return err
				}
				n := 0
				for rows.Next() {
					var id int64
					var name, modified string
					if err := rows.Scan(&id, &name, &modified); err != nil {
						rows.Close()
						return err
					}
					lastID, n = id, n+1
					e := sitemap.Entry{Loc: s.postURL(postType, name, id)}
					if t, ok := parseWPTime(modified); ok {
						e.LastMod = &t
					}
					if err := yield(e); err != nil {
						rows.Close()
						return err
					}
				}
				if err := rows.Err(); err != nil {
					rows.Close()
					return err
				}
				rows.Close()
				if n < batch {
					return nil
				}
			}
		},
	}
}

func termProvider(db *sql.DB, s *site, prefix, taxonomy string) sitemap.Provider {
	query := fmt.Sprintf(
		"SELECT t.term_id, t.slug FROM %sterms t "+
			"JOIN %sterm_taxonomy tt ON t.term_id=tt.term_id "+
			"WHERE tt.taxonomy=? AND tt.count>0 AND t.term_id>? "+
			"ORDER BY t.term_id ASC LIMIT ?", s.prefix, s.prefix)
	return &sitemap.FuncProvider{
		ProviderName: prefix,
		FilePrefix:   prefix,
		EntityKind:   sitemap.KindCategory,
		StreamFunc: func(ctx context.Context, yield func(sitemap.Entry) error) error {
			const batch = 1000
			var lastID int64
			for {
				rows, err := db.QueryContext(ctx, query, taxonomy, lastID, batch)
				if err != nil {
					return err
				}
				n := 0
				for rows.Next() {
					var id int64
					var slug string
					if err := rows.Scan(&id, &slug); err != nil {
						rows.Close()
						return err
					}
					lastID, n = id, n+1
					if err := yield(sitemap.Entry{Loc: s.termURL(taxonomy, slug, id)}); err != nil {
						rows.Close()
						return err
					}
				}
				if err := rows.Err(); err != nil {
					rows.Close()
					return err
				}
				rows.Close()
				if n < batch {
					return nil
				}
			}
		},
	}
}

func authorProvider(db *sql.DB, s *site) sitemap.Provider {
	query := fmt.Sprintf(
		"SELECT u.ID, u.user_nicename FROM %susers u "+
			"WHERE EXISTS (SELECT 1 FROM %sposts p WHERE p.post_author=u.ID "+
			"AND p.post_status='publish' AND p.post_type='post') AND u.ID>? "+
			"ORDER BY u.ID ASC LIMIT ?", s.prefix, s.prefix)
	return &sitemap.FuncProvider{
		ProviderName: "users",
		FilePrefix:   "users",
		EntityKind:   sitemap.Kind("author"),
		StreamFunc: func(ctx context.Context, yield func(sitemap.Entry) error) error {
			const batch = 1000
			var lastID int64
			for {
				rows, err := db.QueryContext(ctx, query, lastID, batch)
				if err != nil {
					return err
				}
				n := 0
				for rows.Next() {
					var id int64
					var nicename string
					if err := rows.Scan(&id, &nicename); err != nil {
						rows.Close()
						return err
					}
					lastID, n = id, n+1
					if err := yield(sitemap.Entry{Loc: s.authorURL(id, nicename)}); err != nil {
						rows.Close()
						return err
					}
				}
				if err := rows.Err(); err != nil {
					rows.Close()
					return err
				}
				rows.Close()
				if n < batch {
					return nil
				}
			}
		},
	}
}

// --- WordPress-style file naming ------------------------------------------

type wpNamer struct{}

func (wpNamer) SitemapName(prefix string, part int, gzip bool) string {
	return fmt.Sprintf("wp-sitemap-%s-%d%s", prefix, part, ext(gzip))
}

func (wpNamer) IndexName(base string, part int, multiple, gzip bool) string {
	if multiple {
		return fmt.Sprintf("%s-%d%s", base, part, ext(gzip))
	}
	return base + ext(gzip)
}

func ext(gzip bool) string {
	if gzip {
		return ".xml.gz"
	}
	return ".xml"
}

// --- Helpers ---------------------------------------------------------------

// parseWPTime parses a WordPress GMT datetime ("2006-01-02 15:04:05"), treating
// zero/empty values as "no lastmod".
func parseWPTime(s string) (time.Time, bool) {
	if s == "" || strings.HasPrefix(s, "0000-00-00") {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func permalinkLabel(structure string) string {
	if structure == "" {
		return "plain"
	}
	return "pretty (" + structure + ")"
}

// --- wp-config.php parsing -------------------------------------------------

type dbConfig struct {
	user, pass, host, name, prefix string
}

func (c dbConfig) dsn() string {
	mc := mysql.NewConfig()
	mc.User = c.user
	mc.Passwd = c.pass
	mc.Net = "tcp"
	mc.Addr = c.host // host[:port]; defaults to :3306 if no port
	if !strings.Contains(mc.Addr, ":") {
		mc.Addr += ":3306"
	}
	mc.DBName = c.name
	mc.Params = map[string]string{"charset": "utf8mb4"}
	mc.Collation = "utf8mb4_unicode_ci"
	mc.Timeout = 10 * time.Second
	return mc.FormatDSN()
}

var (
	reDefine = func(name string) *regexp.Regexp {
		return regexp.MustCompile(`define\(\s*['"]` + name + `['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
	}
	rePrefix = regexp.MustCompile(`\$table_prefix\s*=\s*['"]([^'"]+)['"]`)
	reSafe   = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

func parseWPConfig(path string) (dbConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return dbConfig{}, err
	}
	src := string(data)
	find := func(name string) string {
		if m := reDefine(name).FindStringSubmatch(src); m != nil {
			return m[1]
		}
		return ""
	}
	cfg := dbConfig{
		user:   find("DB_USER"),
		pass:   find("DB_PASSWORD"),
		host:   find("DB_HOST"),
		name:   find("DB_NAME"),
		prefix: "wp_",
	}
	if m := rePrefix.FindStringSubmatch(src); m != nil {
		cfg.prefix = m[1]
	}
	if cfg.host == "" || cfg.host == "localhost" {
		cfg.host = "127.0.0.1"
	}
	if cfg.name == "" || cfg.user == "" {
		return cfg, fmt.Errorf("wp-config.php missing DB_NAME or DB_USER")
	}
	// The prefix is interpolated into SQL, so it must be an identifier.
	if !reSafe.MatchString(cfg.prefix) {
		return cfg, fmt.Errorf("unsafe table prefix %q", cfg.prefix)
	}
	return cfg, nil
}

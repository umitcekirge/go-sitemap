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
// The site URL, permalink structure, category/tag bases and front-page setting
// are read from wp_options, so URLs are correct without extra configuration.
// Permalink structures using %category% are rejected rather than guessed. The password may also be supplied
// via the WP_DB_PASSWORD environment variable to keep it off the command line.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strconv"
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
		timeout  = flag.Duration("timeout", 10*time.Minute, "overall time limit")
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

	if err := run(dataSource, tablePrefix, *outDir, *baseURL, *perFile, *gzip, *timeout); err != nil {
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

func run(dataSource, tablePrefix, outDir, baseOverride string, perFile int, gzip bool, timeout time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
	for _, name := range res.PrunedFiles {
		fmt.Printf("  removed stale %s\n", name)
	}
	if res.PruneErr != nil {
		log.Printf("stale file cleanup: %v", res.PruneErr)
	}
	fmt.Print("\nrobots.txt:\n", sitemap.RobotsSitemapLinesFromResult(res))
	return nil
}

// --- WordPress site model -------------------------------------------------

type site struct {
	home         string // no trailing slash
	structure    string // permalink_structure ("" => plain query URLs)
	prefix       string // table prefix, e.g. "wp_"
	catBase      string // custom category_base, "" for the default
	tagBase      string // custom tag_base, "" for the default
	postsOnFront bool   // show_on_front = "posts": the home page lists posts
	frontPageID  int64  // page_on_front when a static page is the home page
}

// Permalink tags this example can resolve. %category% needs WordPress's
// primary-category logic, so it is rejected rather than guessed.
var supportedTags = map[string]bool{
	"%year%": true, "%monthnum%": true, "%day%": true, "%hour%": true, "%minute%": true,
	"%second%": true, "%postname%": true, "%post_id%": true, "%author%": true,
}

var reTag = regexp.MustCompile(`%[a-z_]+%`)

func loadSite(ctx context.Context, db *sql.DB, prefix string) (*site, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		"SELECT option_name, option_value FROM %soptions WHERE option_name IN "+
			"('home','siteurl','permalink_structure','category_base','tag_base','show_on_front','page_on_front')", prefix))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	opts := map[string]string{}
	for rows.Next() {
		var name, val string
		if err := rows.Scan(&name, &val); err != nil {
			return nil, err
		}
		opts[name] = val
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s := &site{
		prefix:    prefix,
		home:      strings.TrimRight(opts["home"], "/"),
		structure: opts["permalink_structure"],
		catBase:   strings.Trim(opts["category_base"], "/"),
		tagBase:   strings.Trim(opts["tag_base"], "/"),
	}
	if s.home == "" {
		s.home = strings.TrimRight(opts["siteurl"], "/")
	}
	if s.home == "" {
		return nil, fmt.Errorf("could not determine site home URL")
	}
	switch opts["show_on_front"] {
	case "page":
		s.frontPageID, _ = strconv.ParseInt(opts["page_on_front"], 10, 64)
	default:
		s.postsOnFront = true
	}
	for _, tag := range reTag.FindAllString(s.structure, -1) {
		if !supportedTags[tag] {
			return nil, fmt.Errorf("permalink tag %s in %q is not supported by this example", tag, s.structure)
		}
	}
	return s, nil
}

// pretty reports whether the install uses pretty permalinks.
func (s *site) pretty() bool { return s.structure != "" }

// slash mirrors user_trailingslashit: URLs end in "/" when the structure does.
func (s *site) slash(p string) string {
	if strings.HasSuffix(s.structure, "/") {
		return p + "/"
	}
	return p
}

// front is the static part of the structure before the first tag (e.g.
// "/blog/"), which WordPress prepends to term and author URLs.
func (s *site) front() string {
	f := s.structure
	if i := strings.IndexByte(f, '%'); i >= 0 {
		f = f[:i]
	}
	return "/" + strings.Trim(f, "/") + "/"
}

// index reports PATHINFO permalinks ("/index.php/...").
func (s *site) index() bool { return strings.HasPrefix(s.structure, "/index.php/") }

// root is the prefix pages carry: "/index.php/" for PATHINFO permalinks.
func (s *site) root() string {
	if s.index() {
		return "/index.php/"
	}
	return "/"
}

type postRow struct {
	id, parent int64
	name       string
	date       time.Time // post_date, local site time as used in permalinks
	author     string
}

func (s *site) postURL(postType string, p postRow, pagePath func(int64) string) string {
	if p.id == s.frontPageID {
		return s.home + "/"
	}
	if !s.pretty() {
		switch postType {
		case "page":
			return fmt.Sprintf("%s/?page_id=%d", s.home, p.id)
		case "post":
			return fmt.Sprintf("%s/?p=%d", s.home, p.id)
		default:
			return fmt.Sprintf("%s/?post_type=%s&p=%d", s.home, postType, p.id)
		}
	}
	switch postType {
	case "page":
		return s.home + s.root() + s.slash(pagePath(p.id))
	case "post":
		return s.home + strings.NewReplacer(
			"%year%", p.date.Format("2006"), "%monthnum%", p.date.Format("01"), "%day%", p.date.Format("02"),
			"%hour%", p.date.Format("15"), "%minute%", p.date.Format("04"), "%second%", p.date.Format("05"),
			"%postname%", p.name, "%post_id%", strconv.FormatInt(p.id, 10), "%author%", p.author,
		).Replace(s.structure)
	default:
		return s.home + s.front() + postType + "/" + s.slash(p.name)
	}
}

func (s *site) termURL(taxonomy, path, slug string, termID int64) string {
	if s.pretty() {
		base, custom := taxonomy, ""
		switch taxonomy {
		case "category":
			base, custom = "category", s.catBase
		case "post_tag":
			base, custom = "tag", s.tagBase
		}
		// Core drops the front for a custom base, except with PATHINFO.
		prefix := s.front()
		if custom != "" {
			base = custom
			if !s.index() {
				prefix = "/"
			}
		}
		return s.home + prefix + base + "/" + s.slash(path)
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
		return s.home + s.front() + "author/" + s.slash(nicename)
	}
	return fmt.Sprintf("%s/?author=%d", s.home, id)
}

// --- Providers (keyset-paginated streaming) -------------------------------

// streamKeyset runs query in keyset-paginated batches; its last two
// placeholders receive the last seen ID and the batch size. scan handles one
// row and returns its ID.
func streamKeyset(ctx context.Context, db *sql.DB, query string, args []any, scan func(*sql.Rows) (int64, error)) error {
	const batch = 1000
	var lastID int64
	for {
		rows, err := db.QueryContext(ctx, query, append(args[:len(args):len(args)], lastID, batch)...)
		if err != nil {
			return err
		}
		n := 0
		for rows.Next() {
			id, err := scan(rows)
			if err != nil {
				rows.Close()
				return err
			}
			lastID, n = id, n+1
		}
		err = rows.Err()
		rows.Close()
		if err != nil || n < batch {
			return err
		}
	}
}

// hierarchy maps an ID to its parent and slug, for building nested paths.
type hierarchy map[int64]struct {
	parent int64
	slug   string
}

// path joins the slugs from the root down to id, e.g. "about/team".
func (h hierarchy) path(id int64) string {
	var parts []string
	for depth := 0; id != 0 && depth < 100; depth++ {
		n, ok := h[id]
		if !ok {
			break
		}
		parts = append(parts, n.slug)
		id = n.parent
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/")
}

func loadHierarchy(ctx context.Context, db *sql.DB, query string, args ...any) (hierarchy, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	h := hierarchy{}
	for rows.Next() {
		var id, parent int64
		var slug string
		if err := rows.Scan(&id, &parent, &slug); err != nil {
			return nil, err
		}
		h[id] = struct {
			parent int64
			slug   string
		}{parent, slug}
	}
	return h, rows.Err()
}

func postTypeProvider(db *sql.DB, s *site, prefix, postType, kind string) sitemap.Provider {
	query := fmt.Sprintf(
		"SELECT p.ID, p.post_parent, p.post_name, p.post_date, p.post_modified_gmt, COALESCE(u.user_nicename, '') "+
			"FROM %sposts p LEFT JOIN %susers u ON u.ID=p.post_author "+
			"WHERE p.post_type=? AND p.post_status='publish' AND p.post_password='' AND p.ID>? "+
			"ORDER BY p.ID ASC LIMIT ?", s.prefix, s.prefix)
	return &sitemap.FuncProvider{
		ProviderName: prefix,
		FilePrefix:   prefix,
		EntityKind:   sitemap.Kind(kind),
		StreamFunc: func(ctx context.Context, yield func(sitemap.Entry) error) error {
			pages := hierarchy{}
			if postType == "page" {
				// Ancestors count regardless of their status, as in get_page_uri.
				var err error
				pages, err = loadHierarchy(ctx, db, fmt.Sprintf(
					"SELECT ID, post_parent, post_name FROM %sposts WHERE post_type='page'", s.prefix))
				if err != nil {
					return err
				}
				// Like core, list the home page when it shows the latest posts.
				if s.postsOnFront {
					if err := yield(sitemap.Entry{Loc: s.home + "/"}); err != nil {
						return err
					}
				}
			}
			return streamKeyset(ctx, db, query, []any{postType}, func(rows *sql.Rows) (int64, error) {
				var p postRow
				var date, modified string
				if err := rows.Scan(&p.id, &p.parent, &p.name, &date, &modified, &p.author); err != nil {
					return 0, err
				}
				p.date, _ = parseWPTime(date)
				e := sitemap.Entry{Loc: s.postURL(postType, p, pages.path)}
				if t, ok := parseWPTime(modified); ok {
					e.LastMod = &t
				}
				return p.id, yield(e)
			})
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
			terms := hierarchy{}
			if taxonomy == "category" {
				var err error
				terms, err = loadHierarchy(ctx, db, fmt.Sprintf(
					"SELECT t.term_id, tt.parent, t.slug FROM %sterms t "+
						"JOIN %sterm_taxonomy tt ON t.term_id=tt.term_id WHERE tt.taxonomy=?", s.prefix, s.prefix), taxonomy)
				if err != nil {
					return err
				}
			}
			return streamKeyset(ctx, db, query, []any{taxonomy}, func(rows *sql.Rows) (int64, error) {
				var id int64
				var slug string
				if err := rows.Scan(&id, &slug); err != nil {
					return 0, err
				}
				path := slug
				if p := terms.path(id); p != "" {
					path = p
				}
				return id, yield(sitemap.Entry{Loc: s.termURL(taxonomy, path, slug, id)})
			})
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
			return streamKeyset(ctx, db, query, nil, func(rows *sql.Rows) (int64, error) {
				var id int64
				var nicename string
				if err := rows.Scan(&id, &nicename); err != nil {
					return 0, err
				}
				return id, yield(sitemap.Entry{Loc: s.authorURL(id, nicename)})
			})
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

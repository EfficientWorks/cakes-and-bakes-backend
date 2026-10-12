// Command mediamigrate moves images and videos stored in Postgres to
// Cloudflare R2, and points the catalogue at their new addresses.
//
// It reads the same environment as the server (DATABASE_URL, PUBLIC_BASE_URL
// and the R2_* settings). Run it once after R2 is configured:
//
//	go run ./cmd/mediamigrate            # dry run: counts only
//	go run ./cmd/mediamigrate -apply     # copy files and rewrite links
//
// Links are rewritten from PUBLIC_BASE_URL/v1/media/<id> to
// R2_PUBLIC_HOST/images/<id> (and videos likewise). Even links it misses keep
// working: the API redirects /v1/media/* to R2 once R2 is configured. The
// database copies are left in place; drop them yourself once you have checked
// the site.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/config"
	"bakehouse/pkg/db"
	"bakehouse/pkg/media"
)

// mediaColumns are the catalogue columns that hold image or video links.
var mediaColumns = []struct{ table, column string }{
	{"product_images", "url"},
	{"product_categories", "image_url"},
	{"modifier_options", "image_url"},
	{"products", "video_url"},
}

func main() {
	apply := flag.Bool("apply", false, "copy the files and rewrite links (default: dry run)")
	flag.Parse()

	cfg := config.Load()
	r2 := media.R2Config{
		AccountID: cfg.R2AccountID, AccessKey: cfg.R2AccessKey, SecretKey: cfg.R2SecretKey,
		Bucket: cfg.R2Bucket, PublicHost: cfg.R2PublicHost, APIBaseURL: cfg.PublicBaseURL,
	}
	if !r2.Complete() {
		fail("R2 is not configured: set R2_ACCOUNT_ID, R2_ACCESS_KEY, R2_SECRET_KEY, R2_BUCKET, R2_PUBLIC_HOST and PUBLIC_BASE_URL", nil)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fail("connect", err)
	}
	defer pool.Close()

	mode := "dry run"
	if *apply {
		mode = "applying"
	}
	fmt.Printf("%s: database -> R2 bucket %q, served from %s\n", mode, r2.Bucket, r2.PublicHost)

	report, err := media.CopyDatabaseToR2(ctx, pool, r2, !*apply)
	if err != nil {
		fail("copy", err)
	}
	fmt.Printf("images: %d, videos: %d\n", report.Images, report.Videos)

	oldBase := strings.TrimRight(cfg.PublicBaseURL, "/") + "/v1/media/"
	newHost := strings.TrimRight(r2.PublicHost, "/") + "/"
	for _, c := range mediaColumns {
		// Videos first: their old prefix (…/media/videos/) also starts with
		// the image prefix (…/media/).
		for _, swap := range [][2]string{{oldBase + "videos/", newHost + "videos/"}, {oldBase, newHost + "images/"}} {
			n, err := rewrite(ctx, pool, c.table, c.column, swap[0], swap[1], *apply)
			if err != nil {
				fail("rewrite "+c.table+"."+c.column, err)
			}
			if n > 0 {
				fmt.Printf("%s.%s: %d link(s) %s -> %s\n", c.table, c.column, n, swap[0], swap[1])
			}
		}
	}
	if !*apply {
		fmt.Println("nothing changed. Run again with -apply to copy and rewrite.")
	}
}

// rewrite swaps a link prefix in one column, or only counts when dry.
func rewrite(ctx context.Context, pool *pgxpool.Pool, table, column, from, to string, apply bool) (int64, error) {
	where := fmt.Sprintf(`starts_with(%s, $1)`, column)
	if !apply {
		var n int64
		err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s`, table, where), from).Scan(&n)
		return n, err
	}
	tag, err := pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET %s = $2 || substr(%s, length($1) + 1) WHERE %s`, table, column, column, where), from, to)
	return tag.RowsAffected(), err
}

func fail(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
	} else {
		fmt.Fprintln(os.Stderr, what)
	}
	os.Exit(1)
}

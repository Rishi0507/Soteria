// shopctl is a read-mostly CLI for verifying the Shopify client against a
// real store. Reads are safe to run any time; writes are explicit
// subcommands so nobody zeroes stock by accident.
//
//	SHOPIFY_SHOP=soteria-dev.myshopify.com SHOPIFY_ACCESS_TOKEN=shpat_... \
//	  go run ./cmd/shopctl locations
//	  go run ./cmd/shopctl catalog
//	  go run ./cmd/shopctl barcode 012345678905
//	  go run ./cmd/shopctl orders 7          # last 7 days
//	  go run ./cmd/shopctl move <inventoryItemGID> <fromLocationGID> <toLocationGID> <qty>
//	  go run ./cmd/shopctl set  <inventoryItemGID> <locationGID> <qty>
//	  go run ./cmd/shopctl status <productGID> ACTIVE|DRAFT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"

	"soteria/libs/shopify"
)

func main() {
	_ = godotenv.Load()
	if len(os.Args) < 2 {
		usage()
	}
	shop, token := os.Getenv("SHOPIFY_SHOP"), os.Getenv("SHOPIFY_ACCESS_TOKEN")
	var opts []shopify.Option
	if v := os.Getenv("SHOPIFY_API_VERSION"); v != "" {
		opts = append(opts, shopify.WithAPIVersion(v))
	}
	c, err := shopify.New(shop, token, opts...)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	args := os.Args[2:]
	switch os.Args[1] {
	case "locations":
		v, err := c.Locations(ctx)
		print(v, err)
	case "catalog":
		v, err := c.Catalog(ctx)
		print(v, err)
	case "barcode":
		need(args, 1)
		v, err := c.VariantsByBarcode(ctx, args)
		print(v, err)
	case "variant":
		need(args, 1)
		v, err := c.Variant(ctx, args[0])
		print(v, err)
	case "orders":
		days := 7
		if len(args) > 0 {
			days = atoi(args[0])
		}
		v, err := c.OrdersSince(ctx, time.Now().Add(-time.Duration(days)*24*time.Hour))
		print(v, err)
	case "move":
		need(args, 4)
		done(c.MoveAvailable(ctx, args[0], args[1], args[2], atoi(args[3]), "quality_control"))
	case "set":
		need(args, 3)
		done(c.SetAvailable(ctx, args[0], args[1], atoi(args[2]), "quality_control"))
	case "status":
		need(args, 2)
		done(c.SetProductStatus(ctx, args[0], args[1]))
	case "tag":
		need(args, 2)
		done(c.AddTags(ctx, args[0], args[1:]))
	case "untag":
		need(args, 2)
		done(c.RemoveTags(ctx, args[0], args[1:]))
	case "badge":
		need(args, 2)
		done(c.SetMetafields(ctx, []shopify.Metafield{{OwnerID: args[0], Namespace: "soteria", Key: "badge", Value: args[1]}}))
	default:
		usage()
	}
}

func print(v any, err error) {
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func done(err error) {
	if err != nil {
		fatal(err)
	}
	fmt.Println("ok")
}

func need(args []string, n int) {
	if len(args) < n {
		usage()
	}
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		fatal(fmt.Errorf("not a number: %q", s))
	}
	return n
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: shopctl <command> [args]
  reads:  locations | catalog | barcode <upc>... | variant <gid> | orders [days]
  writes: move <itemGID> <fromLocGID> <toLocGID> <qty> | set <itemGID> <locGID> <qty>
          status <productGID> ACTIVE|DRAFT|ARCHIVED | tag <productGID> <tag>... | untag <productGID> <tag>...
          badge <productGID> "<text>"
env: SHOPIFY_SHOP, SHOPIFY_ACCESS_TOKEN, [SHOPIFY_API_VERSION]`)
	os.Exit(2)
}

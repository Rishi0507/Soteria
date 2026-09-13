// Command seedshop prepares a Shopify store for Sotería and checks that it stays
// prepared.
//
// Three things have to be true before a recall can be contained surgically:
//
//  1. the Quarantine location exists, because a hold moves stock there rather
//     than deleting it;
//  2. each variant's Barcode holds a real GTIN, because that is the only key a
//     recall notice can be matched on;
//  3. each variant carries a soteria.lots metafield, because Shopify has no
//     notion of lots and without that ledger a recall can only take the whole SKU.
//
// Subcommands:
//
//	generate  build a seed file from live openFDA recalls, so the demo catalog is
//	          real products with real barcodes and real recalled lot codes
//	apply     make the store match the seed file (idempotent)
//	verify    read-only readiness report: what would break, and why
//
// Env: SHOPIFY_SHOP, SHOPIFY_ACCESS_TOKEN (apply and verify only).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"soteria/libs/core/matching"
	"soteria/libs/shopify"
)

// QuarantineName is the location contaminated lots are moved to. It must match
// containment-service's QUARANTINE_LOCATION.
const QuarantineName = "Quarantine"

// Seed is the desired state of the store.
type Seed struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Source      string        `json:"source"`
	Products    []SeedProduct `json:"products"`
}

// SeedProduct is one product and its lot ledger.
type SeedProduct struct {
	Title   string `json:"title"`
	Vendor  string `json:"vendor"`
	SKU     string `json:"sku"`
	Barcode string `json:"barcode"`
	Price   string `json:"price"`
	// Lots must sum to the stock held at the selling location: the ledger is the
	// claim that a hold acts on, so a ledger that disagrees with stock will either
	// fail the move or quietly under-hold.
	Lots []shopify.Lot `json:"lots"`
	// Provenance records which real recall this product came from, so nobody has
	// to wonder later whether a demo catalog entry was invented.
	Provenance string `json:"provenance,omitempty"`
}

// Units is the stock implied by the ledger.
func (p SeedProduct) Units() int {
	n := 0
	for _, l := range p.Lots {
		n += l.Units
	}
	return n
}

// Validate catches the mistakes that silently break containment.
func (p SeedProduct) Validate() error {
	if strings.TrimSpace(p.Title) == "" {
		return errors.New("title is required")
	}
	if matching.NormalizeGTIN(p.Barcode) == "" {
		return fmt.Errorf("barcode %q is not a valid GTIN: a recall can never match this product", p.Barcode)
	}
	if len(p.Lots) == 0 {
		return errors.New("no lots: containment could only take the whole SKU")
	}
	seen := map[string]bool{}
	for _, l := range p.Lots {
		code := strings.ToUpper(strings.TrimSpace(l.Code))
		if code == "" {
			return errors.New("lot with an empty code")
		}
		if seen[code] {
			return fmt.Errorf("duplicate lot %q", code)
		}
		seen[code] = true
		if l.Units < 0 {
			return fmt.Errorf("lot %q has negative units", code)
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx := context.Background()
	var err error

	switch os.Args[1] {
	case "generate":
		err = runGenerate(ctx, os.Args[2:])
	case "apply":
		err = runApply(ctx, os.Args[2:])
	case "verify":
		err = runVerify(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `seedshop — prepare a Shopify store for Sotería

  seedshop generate [-out seed.json] [-count 3] [-days 45]
      Build a seed from live openFDA recalls (real products, real barcodes,
      real recalled lot codes). No credentials needed.

  seedshop apply [-seed seed.json] [-dry-run]
      Create the Quarantine location, the metafield definitions, and the
      products. Idempotent: safe to re-run.

  seedshop verify
      Read-only readiness report for the configured store.

Env: SHOPIFY_SHOP, SHOPIFY_ACCESS_TOKEN
`)
}

// client builds an Admin API client from the environment.
func client() (*shopify.Client, error) {
	shop, token := os.Getenv("SHOPIFY_SHOP"), os.Getenv("SHOPIFY_ACCESS_TOKEN")
	if shop == "" || token == "" {
		return nil, errors.New("set SHOPIFY_SHOP and SHOPIFY_ACCESS_TOKEN (a token with " +
			"read_products write_products read_inventory write_inventory read_locations)")
	}
	return shopify.New(shop, token)
}

func loadSeed(path string) (Seed, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Seed{}, fmt.Errorf("read seed %s: %w (run `seedshop generate` first)", path, err)
	}
	var s Seed
	if err := json.Unmarshal(raw, &s); err != nil {
		return Seed{}, fmt.Errorf("parse seed %s: %w", path, err)
	}
	if len(s.Products) == 0 {
		return Seed{}, fmt.Errorf("seed %s has no products", path)
	}
	for i, p := range s.Products {
		if err := p.Validate(); err != nil {
			return Seed{}, fmt.Errorf("product %d (%s): %w", i+1, p.Title, err)
		}
	}
	return s, nil
}

func writeSeed(path string, s Seed) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func flags(name string, args []string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	return fs
}

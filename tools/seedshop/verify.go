package main

import (
	"context"
	"fmt"
	"strings"

	"soteria/libs/core/matching"
	"soteria/libs/shopify"
)

// reader is the read-only surface verify needs.
type reader interface {
	Locations(ctx context.Context) ([]shopify.Location, error)
	Catalog(ctx context.Context) ([]shopify.Variant, error)
}

// Report is the readiness of a store, in the order things fail.
type Report struct {
	HasQuarantine bool
	HasSelling    bool
	Variants      int
	WithBarcode   int
	WithLots      int
	Problems      []string
}

// Ready reports whether a recall could be contained surgically today.
func (r Report) Ready() bool {
	return r.HasQuarantine && r.HasSelling && r.WithBarcode > 0 && r.WithLots > 0
}

func runVerify(ctx context.Context, args []string) error {
	fs := flags("verify", args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := client()
	if err != nil {
		return err
	}
	report, err := Verify(ctx, c)
	if err != nil {
		return err
	}
	fmt.Print(report.String())
	if !report.Ready() {
		return fmt.Errorf("store is not ready")
	}
	return nil
}

// Verify inspects the store and explains what would break, rather than only
// saying whether something is missing.
func Verify(ctx context.Context, r reader) (Report, error) {
	var rep Report

	locations, err := r.Locations(ctx)
	if err != nil {
		return rep, fmt.Errorf("list locations: %w", err)
	}
	for _, l := range locations {
		if strings.EqualFold(l.Name, QuarantineName) {
			rep.HasQuarantine = true
		}
		if l.IsActive && l.Fulfills {
			rep.HasSelling = true
		}
	}
	if !rep.HasQuarantine {
		rep.Problems = append(rep.Problems,
			"no Quarantine location: a hold has nowhere to move stock to, so containment cannot run at all")
	}
	if !rep.HasSelling {
		rep.Problems = append(rep.Problems,
			"no active location fulfilling online orders: nothing to move stock from")
	}

	variants, err := r.Catalog(ctx)
	if err != nil {
		return rep, fmt.Errorf("read catalog: %w", err)
	}
	rep.Variants = len(variants)

	var noBarcode, noLots, ledgerDrift []string
	for _, v := range variants {
		name := strings.TrimSpace(v.ProductTitle + " " + v.Title)
		if matching.NormalizeGTIN(v.Barcode) == "" {
			noBarcode = append(noBarcode, name)
			continue
		}
		rep.WithBarcode++

		if len(v.Lots) == 0 {
			noLots = append(noLots, name)
			continue
		}
		rep.WithLots++

		units := 0
		for _, l := range v.Lots {
			units += l.Units
		}
		if stock := v.Available(); units != stock {
			ledgerDrift = append(ledgerDrift, fmt.Sprintf("%s (ledger %d, stock %d)", name, units, stock))
		}
	}

	if len(noBarcode) > 0 {
		rep.Problems = append(rep.Problems, fmt.Sprintf(
			"%d variant(s) have no valid barcode, so no recall can ever match them: %s",
			len(noBarcode), list(noBarcode)))
	}
	if len(noLots) > 0 {
		rep.Problems = append(rep.Problems, fmt.Sprintf(
			"%d variant(s) have no %s.%s ledger, so a recall can only take the whole SKU: %s",
			len(noLots), shopify.MetafieldNamespace, shopify.LotsKey, list(noLots)))
	}
	if len(ledgerDrift) > 0 {
		rep.Problems = append(rep.Problems, fmt.Sprintf(
			"%d variant(s) have a lot ledger that disagrees with stock, so a hold may move the wrong number of units: %s",
			len(ledgerDrift), list(ledgerDrift)))
	}
	return rep, nil
}

func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\nQuarantine location   %s\n", tick(r.HasQuarantine))
	fmt.Fprintf(&b, "Selling location      %s\n", tick(r.HasSelling))
	fmt.Fprintf(&b, "Variants              %d\n", r.Variants)
	fmt.Fprintf(&b, "  with a valid barcode %d\n", r.WithBarcode)
	fmt.Fprintf(&b, "  with a lot ledger    %d\n", r.WithLots)

	if len(r.Problems) == 0 {
		b.WriteString("\nReady: a recall on any of these barcodes will hold only the affected lots.\n")
		return b.String()
	}
	b.WriteString("\nProblems:\n")
	for _, p := range r.Problems {
		fmt.Fprintf(&b, "  - %s\n", p)
	}
	if r.HasQuarantine && r.HasSelling && r.WithBarcode > 0 && r.WithLots == 0 {
		b.WriteString("\nContainment will still work, but only whole-SKU: every unit is pulled, which is\n" +
			"the outcome Sotería exists to avoid.\n")
	}
	return b.String()
}

func tick(ok bool) string {
	if ok {
		return "yes"
	}
	return "NO"
}

func list(items []string) string {
	if len(items) > 3 {
		return strings.Join(items[:3], ", ") + fmt.Sprintf(" and %d more", len(items)-3)
	}
	return strings.Join(items, ", ")
}

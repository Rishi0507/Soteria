package dossier

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// PDF renders the dossier as the document an insurer or regulator receives.
//
// The layout answers, in order: which incident, what was decided and by whom,
// what it cost and what it saved, then the full timeline, then the proof. The
// verification instructions are printed on the document itself, because a PDF
// that says "trust me" is worth nothing to the person auditing it.
func (d Dossier) PDF() ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(fmt.Sprintf("Soteria incident dossier %s", d.IncidentID), true)
	pdf.SetAutoPageBreak(true, 18)
	pdf.AddPage()

	header(pdf, d)
	summary(pdf, d)
	timeline(pdf, d)
	proof(pdf, d)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("dossier: render pdf: %w", err)
	}
	return buf.Bytes(), nil
}

func header(pdf *fpdf.Fpdf, d Dossier) {
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 9, "Recall containment dossier", "", 1, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 5, fmt.Sprintf("Incident %s   ·   generated %s",
		d.IncidentID, d.GeneratedAt.Format(time.RFC1123)), "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(3)

	if strings.HasPrefix(d.TimestampNote, "CHAIN VERIFICATION FAILED") {
		pdf.SetFillColor(253, 226, 226)
		pdf.SetFont("Helvetica", "B", 10)
		pdf.MultiCell(0, 6, wrap("This dossier did not verify. "+d.TimestampNote), "1", "L", true)
		pdf.Ln(2)
	}
}

func summary(pdf *fpdf.Fpdf, d Dossier) {
	s := d.Summary
	section(pdf, "What happened")

	rows := [][2]string{
		{"Hazard", orDash(s.Hazard)},
		{"Classification", orDash(s.Classification)},
		{"Source", orDash(strings.Join(s.Sources, "; "))},
		{"Products", orDash(strings.Join(s.Products, "; "))},
		{"Lots held", orDash(strings.Join(s.LotsHeld, ", "))},
		{"Scope", orDash(s.Scope)},
		{"Decision", fmt.Sprintf("%s by %s", orDash(s.Decision), orDash(s.DecidedBy))},
		{"Confidence / threshold", fmt.Sprintf("%.2f / %.2f", s.Confidence, s.Threshold)},
		{"Units held", fmt.Sprintf("%d", s.UnitsHeld)},
		{"Units left sellable", fmt.Sprintf("%d", s.UnitsLeftSellable)},
		{"Customers offered a choice", fmt.Sprintf("%d", s.CustomersOffered)},
		{"Customers who answered", fmt.Sprintf("%d", s.CustomersAnswered)},
		{"Notifications delivered", fmt.Sprintf("%d", s.NotificationsSent)},
		{"Resale flags", fmt.Sprintf("%d", s.EvasionFlags)},
	}
	for _, r := range rows {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.CellFormat(52, 5.5, r[0], "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(0, 5.5, wrap(r[1]), "", "L", false)
	}

	// The claim the whole product exists to support, stated plainly.
	if s.UnitsHeld > 0 && s.UnitsLeftSellable > 0 {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "I", 9)
		pdf.SetTextColor(20, 100, 60)
		pdf.MultiCell(0, 5, wrap(fmt.Sprintf(
			"Containment was lot-level: %d units were withdrawn and %d units of unaffected stock remained sellable.",
			s.UnitsHeld, s.UnitsLeftSellable)), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}
	if len(s.RedactedFields) > 0 {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.SetTextColor(110, 110, 110)
		pdf.MultiCell(0, 4.5, wrap("Withheld from this document: "+strings.Join(s.RedactedFields, ", ")+
			". Customer contact details are held by the retailer and are not reproduced here."), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(3)
}

func timeline(pdf *fpdf.Fpdf, d Dossier) {
	section(pdf, fmt.Sprintf("Timeline (%d events)", d.EventCount))

	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetFillColor(240, 240, 240)
	pdf.CellFormat(9, 6, "#", "1", 0, "C", true, 0, "")
	pdf.CellFormat(34, 6, "When (UTC)", "1", 0, "L", true, 0, "")
	pdf.CellFormat(52, 6, "Event", "1", 0, "L", true, 0, "")
	pdf.CellFormat(0, 6, "Detail", "1", 1, "L", true, 0, "")

	pdf.SetFont("Helvetica", "", 8)
	for _, e := range d.Timeline {
		detail := wrap(e.Detail)
		lines := pdf.SplitLines([]byte(detail), 95)
		height := float64(max(1, len(lines))) * 4.2

		x, y := pdf.GetX(), pdf.GetY()
		pdf.CellFormat(9, height, fmt.Sprintf("%d", e.Seq), "1", 0, "C", false, 0, "")
		pdf.CellFormat(34, height, e.At.UTC().Format("2006-01-02 15:04:05"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(52, height, truncate(e.Event, 34), "1", 0, "L", false, 0, "")
		pdf.MultiCell(0, 4.2, detail, "1", "L", false)
		pdf.SetXY(x, y+height)
	}
	pdf.Ln(3)
}

func proof(pdf *fpdf.Fpdf, d Dossier) {
	section(pdf, "Proof of integrity")

	pdf.SetFont("Helvetica", "", 9)
	pdf.MultiCell(0, 5, wrap(
		"Every event above is hash-chained to the one before it. Each record's hash covers its "+
			"position, identity, timestamp, producer and payload, together with the previous record's "+
			"hash. Altering, reordering, inserting or removing any record changes every hash after it, "+
			"so the chain head below fixes the entire sequence."), "", "L", false)
	pdf.Ln(2)

	kv(pdf, "Chain head (sha256)", d.ContentHash)
	kv(pdf, "Events", fmt.Sprintf("%d", d.EventCount))
	kv(pdf, "Incident opened", d.OpenedAt.UTC().Format(time.RFC3339))
	kv(pdf, "Last event", d.ClosedAt.UTC().Format(time.RFC3339))

	switch {
	case d.TimestampProof != "":
		kv(pdf, "RFC 3161 token", truncate(d.TimestampProof, 88))
		pdf.SetFont("Helvetica", "I", 8)
		pdf.MultiCell(0, 4.5, wrap("The token above is a timestamp authority's signature over the chain head, "+
			"proving this content existed at that time and was not composed later."), "", "L", false)
	default:
		pdf.SetFont("Helvetica", "I", 8)
		pdf.SetTextColor(150, 90, 0)
		pdf.MultiCell(0, 4.5, wrap("No external timestamp: "+orDash(d.TimestampNote)+
			". The chain proves internal consistency but is not independently anchored in time."), "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}

	pdf.Ln(2)
	pdf.SetFont("Helvetica", "B", 9)
	pdf.CellFormat(0, 5, "How to verify this document", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 8)
	pdf.MultiCell(0, 4.5, wrap(
		"Request the machine-readable dossier (JSON) for this incident, recompute each record hash "+
			"from its fields, confirm each matches its successor's prev_hash, and confirm the final "+
			"hash equals the chain head printed above. Any discrepancy identifies the first altered record."),
		"", "L", false)
}

// ---------------------------------------------------------------- helpers

func section(pdf *fpdf.Fpdf, title string) {
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetDrawColor(200, 200, 200)
	pdf.CellFormat(0, 7, title, "B", 1, "L", false, 0, "")
	pdf.Ln(1.5)
}

func kv(pdf *fpdf.Fpdf, k, v string) {
	pdf.SetFont("Helvetica", "B", 9)
	pdf.CellFormat(52, 5.5, k, "", 0, "L", false, 0, "")
	pdf.SetFont("Courier", "", 8)
	pdf.MultiCell(0, 5.5, v, "", "L", false)
}

// wrap keeps text inside the core PDF fonts, which are Latin-1 only: a smart
// quote or an accented character from a recall notice would otherwise render as
// mojibake in a legal document.
func wrap(s string) string {
	repl := strings.NewReplacer(
		"‘", "'", "’", "'", "“", `"`, "”", `"`,
		"–", "-", "—", "-", "…", "...", " ", " ", "•", "-",
	)
	s = repl.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 256 {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

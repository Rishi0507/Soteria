// Package rasff ingests EU RASFF (Rapid Alert System for Food and Feed)
// notifications.
//
// There is no official public RASFF API. The RASFF Window portal
// (https://webgate.ec.europa.eu/rasff-window/screen/search) is an Angular
// SPA over an internal backend at /rasff-window/backend whose search
// endpoint lives in a lazy-loaded bundle and is not documented or
// guaranteed. Two modes therefore exist:
//
//   - file: read notification exports (JSON or CSV, the portal's own export
//     columns) dropped into a directory. Deterministic and demoable.
//   - http: reserved for a verified portal-backend adapter. Fails fast at
//     startup until implemented so a misconfiguration is never silent.
//
// Both produce the same Items; only the transport differs.
package rasff

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"soteria/libs/feedkit/source"
)

// Notification is the canonical RASFF record after header normalization.
// Field names follow the RASFF Window export columns.
type Notification struct {
	Reference          string `json:"reference"`
	Date               string `json:"date"`
	NotifyingCountry   string `json:"notifyingCountry"`
	Classification     string `json:"classification"` // alert | information for attention | information for follow-up | border rejection
	NotificationType   string `json:"notificationType"`
	Subject            string `json:"subject"`
	ProductCategory    string `json:"productCategory"`
	Product            string `json:"product"`
	Hazards            string `json:"hazards"`
	RiskDecision       string `json:"riskDecision"`
	DistributionStatus string `json:"distributionStatus"`
	Origin             string `json:"origin"`
	Distribution       string `json:"distribution"` // countries the product reached
	URL                string `json:"url"`
}

// aliases maps normalized header names (lowercase, alphanumerics only) to
// canonical fields, covering the portal's export headers and common
// hand-written variants.
var aliases = map[string]string{
	"reference": "reference", "notificationreference": "reference", "ref": "reference",
	"date": "date", "notificationdate": "date", "dateofnotification": "date", "validationdate": "date",
	"notifyingcountry": "notifyingCountry", "notifiedby": "notifyingCountry", "country": "notifyingCountry",
	"classification": "classification", "notificationclassification": "classification",
	"type": "notificationType", "notificationtype": "notificationType", "basis": "notificationType",
	"subject": "subject", "title": "subject",
	"productcategory": "productCategory", "category": "productCategory",
	"product": "product", "productname": "product",
	"hazards": "hazards", "hazard": "hazards", "hazardcategory": "hazards",
	"riskdecision": "riskDecision", "risk": "riskDecision", "seriousness": "riskDecision",
	"distributionstatus": "distributionStatus",
	"origin":             "origin", "countryoforigin": "origin", "originofproduct": "origin",
	"distribution": "distribution", "distributionto": "distribution", "distributedto": "distribution", "distributioncountries": "distribution",
	"url": "url", "link": "url",
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeKey(k string) string { return nonAlnum.ReplaceAllString(strings.ToLower(k), "") }

// fromRow builds a Notification from a header→value map with arbitrary
// header spellings.
func fromRow(row map[string]string) Notification {
	var n Notification
	set := map[string]*string{
		"reference": &n.Reference, "date": &n.Date, "notifyingCountry": &n.NotifyingCountry,
		"classification": &n.Classification, "notificationType": &n.NotificationType, "subject": &n.Subject,
		"productCategory": &n.ProductCategory, "product": &n.Product, "hazards": &n.Hazards,
		"riskDecision": &n.RiskDecision, "distributionStatus": &n.DistributionStatus, "origin": &n.Origin,
		"distribution": &n.Distribution, "url": &n.URL,
	}
	for k, v := range row {
		if field, ok := aliases[normalizeKey(k)]; ok {
			if p := set[field]; p != nil && *p == "" {
				*p = strings.TrimSpace(v)
			}
		}
	}
	return n
}

// ParseJSON decodes an array of notification objects (or {"data":[...]}).
func ParseJSON(r io.Reader) ([]Notification, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		var wrapped struct {
			Data []map[string]any `json:"data"`
		}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil || wrapped.Data == nil {
			return nil, fmt.Errorf("rasff: decode json: %w", err)
		}
		rows = wrapped.Data
	}
	out := make([]Notification, 0, len(rows))
	for _, raw := range rows {
		row := make(map[string]string, len(raw))
		for k, v := range raw {
			switch x := v.(type) {
			case string:
				row[k] = x
			case nil:
			default:
				b, _ := json.Marshal(x)
				row[k] = string(b)
			}
		}
		out = append(out, fromRow(row))
	}
	return out, nil
}

// ParseCSV decodes a header-row CSV (comma or semicolon separated, as the
// portal exports depending on locale).
func ParseCSV(r io.Reader) ([]Notification, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\uFEFF") // Excel BOM
	sep := ','
	if head, _, _ := strings.Cut(text, "\n"); strings.Count(head, ";") > strings.Count(head, ",") {
		sep = ';'
	}
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = sep
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("rasff: decode csv: %w", err)
	}
	if len(records) < 1 {
		return nil, errors.New("rasff: csv has no header row")
	}
	header := records[0]
	out := make([]Notification, 0, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) == 0 || (len(rec) == 1 && strings.TrimSpace(rec[0]) == "") {
			continue
		}
		row := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		out = append(out, fromRow(row))
	}
	return out, nil
}

// ToItem projects a Notification into a feed Item.
func ToItem(n Notification) (source.Item, error) {
	if n.Reference == "" {
		return source.Item{}, fmt.Errorf("rasff: notification without reference (subject %q)", n.Subject)
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return source.Item{}, err
	}
	title := n.Subject
	if title == "" {
		title = strings.TrimSpace(strings.Join(nonEmpty(n.Hazards, "in", n.Product), " "))
	}
	if title == "" {
		title = "RASFF notification " + n.Reference
	}
	url := n.URL
	if url == "" {
		url = "https://webgate.ec.europa.eu/rasff-window/screen/notification/" + n.Reference
	}
	distribution := n.Distribution
	if distribution == "" {
		distribution = n.DistributionStatus
	}
	classification := n.Classification
	if n.RiskDecision != "" {
		classification = strings.TrimSpace(strings.Join(nonEmpty(classification, "/", n.RiskDecision), " "))
	}
	product := n.Product
	if product == "" {
		product = n.ProductCategory
	}
	if n.Origin != "" {
		product = strings.TrimSpace(product + " (origin: " + n.Origin + ")")
	}
	return source.Item{
		SourceID:    n.Reference,
		SourceURL:   url,
		PublishedAt: parseDate(n.Date),
		Normalized: source.Normalized{
			Title:              title,
			Firm:               "", // RASFF public data does not name operators
			ProductDescription: product,
			Reason:             n.Hazards,
			Classification:     classification,
			Distribution:       distribution,
			Country:            "EU",
		},
		Raw: raw,
	}, nil
}

func nonEmpty(parts ...string) []string {
	out := parts[:0:0]
	for i, p := range parts {
		// drop connector words when a neighbor is empty
		if p == "in" || p == "/" {
			if i == 0 || i == len(parts)-1 || parts[i-1] == "" || parts[i+1] == "" {
				continue
			}
		}
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var dateLayouts = []string{"2006-01-02", "02/01/2006", "02-01-2006", "2006-01-02T15:04:05", "2006-01-02T15:04:05Z07:00", "02 Jan 2006", "2 Jan 2006"}

func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

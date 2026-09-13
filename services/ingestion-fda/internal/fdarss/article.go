package fdarss

import (
	"context"
	"html"
	"regexp"
	"strings"
	"sync"

	"soteria/libs/feedkit/source"
)

// The RSS description is a ~300-character blurb. The linked press-release
// page carries the whole announcement — UPCs, lot codes, best-by dates,
// states, and a structured summary (company, brand, product, reason). Since
// press releases precede the openFDA enforcement report by days, fetching
// the page is what lets the extractor act on the earliest signal.

// MaxArticleChars caps the text carried in the event.
const MaxArticleChars = 12000

// Article is the structured reading of a press-release page.
type Article struct {
	Text    string // announcement body + summary, whitespace-normalized
	Company string
	Brand   string
	Product string
	Reason  string
}

var (
	mainRe    = regexp.MustCompile(`(?s)<main[^>]*>.*?</main>`)
	dropRe    = regexp.MustCompile(`(?s)<(script|style|nav|header|footer|svg|noscript)[^>]*>.*?</(script|style|nav|header|footer|svg|noscript)>`)
	blockRe   = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|dt|dd|section|table)>|<br\s*/?>`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
	spacesRe  = regexp.MustCompile(`[ \t\x{00a0}]+`)
	blankRe   = regexp.MustCompile(`\n\s*\n+`)
	summaryRe = map[string]*regexp.Regexp{
		"company": regexp.MustCompile(`(?m)^Company Name:\s*\n\s*(.+)$`),
		"brand":   regexp.MustCompile(`(?m)^Brand Name:\s*\n(?:\s*Brand Name\(s\)\s*\n)?\s*(.+)$`),
		"product": regexp.MustCompile(`(?m)^Product Description:\s*\n(?:\s*Product Description\s*\n)?\s*(.+)$`),
		"reason":  regexp.MustCompile(`(?m)^Reason for Announcement:\s*\n(?:\s*Recall Reason Description\s*\n)?\s*(.+)$`),
	}
)

// ParseArticle turns an FDA press-release HTML page into an Article.
func ParseArticle(page []byte) Article {
	h := string(page)
	if m := mainRe.FindString(h); m != "" {
		h = m
	}
	h = dropRe.ReplaceAllString(h, " ")
	h = blockRe.ReplaceAllString(h, "\n")
	h = tagRe.ReplaceAllString(h, " ")
	t := html.UnescapeString(h)
	t = spacesRe.ReplaceAllString(t, " ")
	lines := strings.Split(t, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	t = strings.TrimSpace(blankRe.ReplaceAllString(strings.Join(lines, "\n"), "\n"))

	a := Article{}
	pick := func(key string) string {
		if m := summaryRe[key].FindStringSubmatch(t); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
		return ""
	}
	a.Company, a.Brand, a.Product, a.Reason = pick("company"), pick("brand"), pick("product"), pick("reason")

	// Keep from the summary through the announcement; drop the site chrome
	// that follows ("Follow FDA", photo streams...).
	if i := strings.Index(t, "Company Announcement Date"); i >= 0 {
		t = t[i:]
	}
	for _, stop := range []string{"\nContent current as of:", "\nFollow FDA", "\nProduct Photos\n"} {
		if i := strings.Index(t, stop); i > 0 {
			t = t[:i]
		}
	}
	if len(t) > MaxArticleChars {
		t = t[:MaxArticleChars]
	}
	a.Text = strings.TrimSpace(t)
	return a
}

// articleCache remembers pages already fetched in this process. The feed
// holds ~20 items, so at most that many pages are ever fetched on boot and
// then only new items cost a request.
type articleCache struct {
	mu sync.Mutex
	m  map[string]Article
}

func (c *articleCache) get(k string) (Article, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.m[k]
	return a, ok
}

func (c *articleCache) put(k string, a Article, keep map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]Article{}
	}
	c.m[k] = a
	for key := range c.m {
		if !keep[key] {
			delete(c.m, key) // item fell off the feed; never needed again
		}
	}
}

// enrich fetches each item's press-release page and folds the article into
// the item. A failed page fetch leaves the item as the blurb (never fatal —
// the notice must still flow).
func (s *Source) enrich(ctx context.Context, items []source.Item) {
	keep := make(map[string]bool, len(items))
	for _, it := range items {
		keep[it.SourceID] = true
	}
	for i := range items {
		it := &items[i]
		if it.SourceURL == "" {
			continue
		}
		a, ok := s.cache.get(it.SourceID)
		if !ok {
			page, err := s.Client.Get(ctx, it.SourceURL, map[string]string{"Accept": "text/html"})
			if err != nil {
				continue
			}
			a = ParseArticle(page)
			if a.Text == "" {
				continue
			}
			s.cache.put(it.SourceID, a, keep)
		}
		applyArticle(it, a)
	}
}

// applyArticle merges the article into the item's normalized fields and raw record.
func applyArticle(it *source.Item, a Article) {
	if a.Text != "" {
		it.Normalized.ProductDescription = a.Text
	}
	if a.Company != "" {
		it.Normalized.Firm = a.Company
	}
	if a.Reason != "" {
		it.Normalized.Reason = a.Reason
	}
	// Raw stays a JSON object; add the article alongside the RSS fields.
	raw := strings.TrimSpace(string(it.Raw))
	if strings.HasSuffix(raw, "}") {
		extra := `,"article":` + jsonString(a.Text) + `,"summary":{"company":` + jsonString(a.Company) + `,"brand":` + jsonString(a.Brand) +
			`,"product":` + jsonString(a.Product) + `,"reason":` + jsonString(a.Reason) + `}}`
		it.Raw = []byte(raw[:len(raw)-1] + extra)
	}
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte("0123456789abcdef"[r>>4])
				b.WriteByte("0123456789abcdef"[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

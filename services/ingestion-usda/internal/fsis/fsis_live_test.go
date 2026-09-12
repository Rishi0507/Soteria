package fsis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestMultiStringAcceptsBothShapes: FSIS returns several fields as a string on
// one record and as an array on the next. One array-valued record used to fail
// the entire poll.
func TestMultiStringAcceptsBothShapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"New Jersey"`, "New Jersey"},
		{`["New Jersey","Utah"]`, "New Jersey; Utah"},
		{`["New Jersey","","  Utah  "]`, "New Jersey; Utah"},
		{`[]`, ""},
		{`""`, ""},
	}
	for _, c := range cases {
		var got multiString
		if err := json.Unmarshal([]byte(c.in), &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", c.in, err)
		}
		if got.String() != c.want {
			t.Errorf("Unmarshal(%s) = %q, want %q", c.in, got, c.want)
		}
	}

	var bad multiString
	if err := json.Unmarshal([]byte(`{"a":1}`), &bad); err == nil {
		t.Error("an object is neither string nor []string and must fail")
	}
}

// TestParseHandlesArrayValuedRecord is the regression for the live payload that
// broke the poll: every array-typed field populated at once.
func TestParseHandlesArrayValuedRecord(t *testing.T) {
	body := []byte(`[{
		"field_recall_number":"019-020-2026",
		"field_title":"Firm Recalls Pork Products Due to Possible Listeria Contamination",
		"field_recall_date":"2026-09-06",
		"field_recall_classification":"Class I",
		"field_states":["Texas","Idaho"],
		"field_product_items":["<p>Cases of GUANCIALE with LOT: 263311US</p>"],
		"field_recall_reason":["Product Contamination"],
		"field_establishment":[],
		"field_press_release":[],
		"field_summary":"<p>WASHINGTON, Sept. 06, 2026</p>"
	}]`)

	items, err := Parse(body, time.Time{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	n := items[0].Normalized
	if n.Distribution != "Texas; Idaho" {
		t.Errorf("distribution = %q, want the states joined", n.Distribution)
	}
	if !strings.Contains(n.ProductDescription, "263311US") {
		t.Errorf("product description lost the lot code: %q", n.ProductDescription)
	}
	if !strings.Contains(n.Reason, "Product Contamination") {
		t.Errorf("reason = %q", n.Reason)
	}
}

// TestRequestHeadersCarryTheEdgeFingerprint: www.fsis.usda.gov answers 403
// without these, and Accept-Encoding must stay unset so Go's transport can add
// gzip and transparently decompress.
func TestRequestHeadersCarryTheEdgeFingerprint(t *testing.T) {
	h := requestHeaders()
	for _, k := range []string{"User-Agent", "Accept-Language", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest"} {
		if h[k] == "" {
			t.Errorf("missing required header %s", k)
		}
	}
	if _, set := h["Accept-Encoding"]; set {
		t.Error("Accept-Encoding must not be set by hand")
	}
	t.Setenv("FSIS_USER_AGENT", "custom/1.0")
	if got := requestHeaders()["User-Agent"]; got != "custom/1.0" {
		t.Errorf("FSIS_USER_AGENT override ignored, got %q", got)
	}
}

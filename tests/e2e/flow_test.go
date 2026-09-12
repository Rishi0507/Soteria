// Package e2e wires the three core domain services onto one bus and walks the
// whole recall chain end to end:
//
//	recall.raw.received.v1 -> lot.resolved.v1 -> containment.action.{proposed,taken}.v1
//	                       -> order.rescue.proposed.v1 -> order.rescue.confirmed.v1
//
// The ingestion services and the customer-facing UIs are represented by the events
// they produce, exactly as /contracts defines them.
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/libs/core/offacts"
	"soteria/libs/core/shopify"

	containmentapi "soteria/services/containment-service/api"
	"soteria/services/containment-service/containment"
	rescueapi "soteria/services/order-rescue-service/api"
	"soteria/services/order-rescue-service/orders"
	"soteria/services/order-rescue-service/rescue"
	"soteria/services/resolution-service/catalog"
	"soteria/services/resolution-service/resolver"
)

// The shape below is what the ingestion services actually publish: a flat message
// whose normalized fields are verbatim feed text, with all parsing left to this layer.
const (
	noticeTitle = "Sunfield Farms recalls Chewy Granola Bars 12 ct for undeclared peanut"
	noticeDesc  = "Sunfield Farms Chewy Granola Bars 12 ct, UPC 0 41196 91053 7, distributed in CA, OR and WA."
	noticeCodes = "Lot Code: 8H-1132, 8H-1133 printed on the end flap. No other lots are affected."
)

type harness struct {
	bus       *bus.InMem
	inventory *shopify.Fake
	orders    *orders.Memory
	resolve   *resolver.Resolver
	resStore  *resolver.Store
	contain   *containment.Service
	rescue    *rescue.Service
}

func newHarness(t *testing.T, cfg containment.Config) *harness {
	t.Helper()
	b := bus.NewInMem(nil)

	cat := catalog.NewStatic(
		catalog.Product{GTIN: "041196910537", SKU: "SF-GB-12", Brand: "Sunfield Farms",
			ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
			LotCodes:     []string{"8H-1132", "8H-1133", "8H-2000"}},
		catalog.Product{GTIN: "012345678905", SKU: "HL-GB-12", Brand: "Harvest Lane",
			ProductTitle: "Harvest Lane Oat and Honey Granola Bars 12 ct", LotCodes: []string{"K221"}},
	)
	resStore := resolver.NewStore()
	resStore.SetLedger(func(ctx context.Context, gtin string) []string {
		p, ok := cat.ByGTIN(ctx, gtin)
		if !ok {
			return nil
		}
		return p.LotCodes
	})
	res := resolver.New(cat, b, resStore, nil)

	inv := shopify.NewFake(
		shopify.InventoryLot{GTIN: "00041196910537", SKU: "SF-GB-12", LotCode: "8H-1132", Units: 40},
		shopify.InventoryLot{GTIN: "00041196910537", SKU: "SF-GB-12", LotCode: "8H-1133", Units: 25},
		shopify.InventoryLot{GTIN: "00041196910537", SKU: "SF-GB-12", LotCode: "8H-2000", Units: 60},
	)
	cont := containment.New(containment.NewStore(cfg), inv, b, nil)

	ord := orders.NewMemory(
		order("ORD-1001", orders.StatusPending, "cust-77", "li-1", "8H-1132", 2),
		order("ORD-1002", orders.StatusPending, "cust-91", "li-2", "8H-2000", 1),
		order("ORD-1003", orders.StatusShipped, "cust-12", "li-3", "8H-1133", 3),
	)
	subs := rescue.NewMemoryCatalog(
		rescue.Substitute{GTIN: "00012345678905", SKU: "HL-GB-12", ProductTitle: "Harvest Lane Oat and Honey Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: money(449), InStock: 120},
		rescue.Substitute{GTIN: "00073123456788", SKU: "NT-GB-12", ProductTitle: "Nutty Trail Peanut Crunch Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: money(449), InStock: 80},
		rescue.Substitute{GTIN: "00099999999999", SKU: "PR-GB-12", ProductTitle: "Premium Seed Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: money(699), InStock: 40},
		rescue.Substitute{GTIN: "00041196910537", SKU: "SF-GB-12", ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: money(449), InStock: 0},
	)
	allergens := offacts.NewStatic(
		offacts.Product{GTIN: "00041196910537", Allergens: []string{"en:gluten"}},
		offacts.Product{GTIN: "00012345678905", Allergens: []string{"en:gluten"}},
		offacts.Product{GTIN: "00073123456788", Allergens: []string{"en:gluten", "en:peanuts"}},
		offacts.Product{GTIN: "00099999999999", Allergens: []string{"en:gluten"}, Traces: []string{"en:nuts"}},
	)
	resc := rescue.New(ord, subs, allergens, b, "test-secret", nil)

	for _, reg := range []func(bus.Consumer) error{res.Register, cont.Register, resc.Register} {
		if err := reg(b); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	return &harness{bus: b, inventory: inv, orders: ord, resolve: res, resStore: resStore, contain: cont, rescue: resc}
}

func order(id, status, customer, lineID, lot string, qty int) orders.Order {
	return orders.Order{
		OrderID:  id,
		Status:   status,
		Customer: events.Customer{CustomerID: customer, Email: customer + "@example.com"},
		LineItems: []orders.LineItem{{
			LineItemID:   lineID,
			GTIN:         "00041196910537",
			SKU:          "SF-GB-12",
			ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
			LotCode:      lot,
			Quantity:     qty,
			UnitPrice:    money(449),
		}},
	}
}

func money(minor int) events.Money { return events.Money{AmountMinor: minor, Currency: "USD"} }

// publishNotice stands in for ingestion-fda: it publishes the flat
// recall.raw.received.v1 message exactly as the ingestion contract defines it,
// routed on the real key, and lets InboundEnvelope adapt it at the boundary.
func publishNotice(t *testing.T, h *harness) {
	t.Helper()
	published := time.Now().UTC().Add(-10 * time.Minute)
	notice := events.RecallRawReceived{
		EventID:     events.NewID(),
		EventType:   "recall.raw.received",
		Version:     1,
		OccurredAt:  time.Now().UTC(),
		Producer:    "ingestion-fda",
		Source:      "fda_enforcement",
		SourceID:    "F-0921-2026",
		SourceURL:   strptr("https://api.fda.gov/food/enforcement/F-0921-2026"),
		PublishedAt: &published,
		Normalized: events.RecallNormalized{
			Title:              noticeTitle,
			Firm:               strptr("Sunfield Farms"),
			ProductDescription: strptr(noticeDesc),
			Reason:             strptr("undeclared peanut"),
			CodeInfo:           strptr(noticeCodes),
			Classification:     strptr("Class I"),
			Distribution:       strptr("CA, OR, WA"),
			Country:            "US",
		},
		Raw: json.RawMessage(`{"recall_number":"F-0921-2026"}`),
	}
	body, err := json.Marshal(notice)
	if err != nil {
		t.Fatalf("marshal notice: %v", err)
	}
	env, err := events.InboundEnvelope(events.TypeRecallRawReceived, body)
	if err != nil {
		t.Fatalf("inbound envelope: %v", err)
	}
	if err := h.bus.Publish(context.Background(), env); err != nil {
		t.Fatalf("publish notice: %v", err)
	}
}

func strptr(s string) *string { return &s }

// TestHighConfidenceFlowIsSurgicalAndRescuesOrders is the happy path end to end.
func TestHighConfidenceFlowIsSurgicalAndRescuesOrders(t *testing.T) {
	h := newHarness(t, containment.DefaultConfig())
	publishNotice(t, h)

	if dl := h.bus.DeadLetters(); len(dl) > 0 {
		t.Fatalf("unexpected dead letters: %v", dl)
	}

	// 1. Resolution reached lot level.
	resolved := h.bus.PublishedOfType(events.TypeLotResolved)
	if len(resolved) != 1 {
		t.Fatalf("expected 1 lot.resolved.v1, got %d", len(resolved))
	}
	var lr events.LotResolved
	mustDecode(t, resolved[0], &lr)
	if lr.Scope != events.ScopeLot {
		t.Fatalf("expected LOT scope, got %s", lr.Scope)
	}
	if lr.Confidence < 0.85 {
		t.Fatalf("expected high confidence from a declared UPC, got %.3f", lr.Confidence)
	}
	if got := lr.Matches[0].LotCodes; len(got) != 2 {
		t.Fatalf("expected the two recalled lots, got %v", got)
	}
	if len(lr.Allergens) == 0 || lr.Allergens[0] != "peanuts" {
		t.Fatalf("expected the peanut hazard on the incident, got %v", lr.Allergens)
	}

	// 2. Containment auto-held without a human, and only the recalled lots.
	taken := h.bus.PublishedOfType(events.TypeContainmentTaken)
	if len(taken) != 1 {
		t.Fatalf("expected 1 containment.action.taken.v1, got %d", len(taken))
	}
	var ct events.ContainmentActionTaken
	mustDecode(t, taken[0], &ct)
	if ct.Decision != events.DecisionAutoHold {
		t.Fatalf("expected AUTO_HOLD, got %s", ct.Decision)
	}
	if len(h.bus.PublishedOfType(events.TypeContainmentProposed)) != 0 {
		t.Fatalf("a high-confidence incident must not sit in the review queue")
	}
	if ct.Results[0].UnitsHeld != 65 || ct.Results[0].UnitsLeftSellable != 60 {
		t.Fatalf("expected 65 units held and 60 still sellable, got %+v", ct.Results[0])
	}
	for _, lot := range h.inventory.Lots() {
		if lot.LotCode == "8H-2000" && lot.Held {
			t.Fatalf("clean lot 8H-2000 was held - containment was not surgical")
		}
	}

	// 3. The storefront badge answers per lot, not per SKU.
	if v := h.resStore.LotStatus(context.Background(), "041196910537", "8H-1132").Verdict; v != resolver.VerdictAffected {
		t.Fatalf("recalled lot verdict = %s, want AFFECTED", v)
	}
	if v := h.resStore.LotStatus(context.Background(), "041196910537", "8H-2000").Verdict; v != resolver.VerdictSafe {
		t.Fatalf("clean lot verdict = %s, want SAFE", v)
	}

	// 4. Only the in-flight order holding an affected lot was rescued.
	proposals := h.bus.PublishedOfType(events.TypeOrderRescueProposed)
	if len(proposals) != 1 {
		t.Fatalf("expected exactly 1 rescue (ORD-1001), got %d", len(proposals))
	}
	var rp events.OrderRescueProposed
	mustDecode(t, proposals[0], &rp)
	if rp.OrderID != "ORD-1001" {
		t.Fatalf("rescued the wrong order: %s", rp.OrderID)
	}

	// 5. Substitutes are same-price and allergen-safe; the peanut one is refused.
	var subs []events.RescueOption
	for _, o := range rp.Options {
		if o.Kind == events.OptionSubstitute {
			subs = append(subs, o)
		}
	}
	if len(subs) != 1 || subs[0].GTIN != "00012345678905" {
		t.Fatalf("expected only the allergen-safe same-price substitute, got %+v", subs)
	}
	if !hasOption(rp.Options, "refund") || !hasOption(rp.Options, "cancel") {
		t.Fatalf("customer must always keep a refund and a cancel path: %+v", rp.Options)
	}

	// 6. Nothing is swapped until the customer explicitly confirms.
	if swapped := swappedTo(h, "ORD-1001", "li-1"); swapped != "" {
		t.Fatalf("order was swapped before confirmation (to %s)", swapped)
	}

	// 7. Customer confirms through the storefront API.
	srv := httptest.NewServer(rescueapi.New(h.rescue, nil).Routes())
	defer srv.Close()
	body := map[string]string{"option_id": subs[0].OptionID, "consent_token": h.rescue.ConsentToken(rp.RescueID)}
	resp := post(t, srv.URL+"/v1/rescues/"+rp.RescueID+"/confirm", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm returned %d", resp.StatusCode)
	}

	if got := swappedTo(h, "ORD-1001", "li-1"); got != "00012345678905" {
		t.Fatalf("line was not swapped to the confirmed substitute, got %q", got)
	}
	confirmed := h.bus.PublishedOfType(events.TypeOrderRescueConfirmed)
	if len(confirmed) != 1 {
		t.Fatalf("expected 1 order.rescue.confirmed.v1, got %d", len(confirmed))
	}
	rec, _ := h.rescue.Get(rp.RescueID)
	if rec.Status != rescue.StatusConfirmed {
		t.Fatalf("rescue status = %s, want CONFIRMED", rec.Status)
	}
	if dl := h.bus.DeadLetters(); len(dl) > 0 {
		t.Fatalf("unexpected dead letters: %v", dl)
	}
}

// TestBelowThresholdWaitsForAHuman covers the ops-console review path.
func TestBelowThresholdWaitsForAHuman(t *testing.T) {
	cfg := containment.DefaultConfig()
	cfg.AutoHoldThreshold = 0.995 // nothing can clear this
	h := newHarness(t, cfg)
	publishNotice(t, h)

	proposed := h.bus.PublishedOfType(events.TypeContainmentProposed)
	if len(proposed) != 1 {
		t.Fatalf("expected the action to be queued for review, got %d proposals", len(proposed))
	}
	if len(h.bus.PublishedOfType(events.TypeContainmentTaken)) != 0 {
		t.Fatalf("nothing may be held before a human confirms")
	}
	for _, lot := range h.inventory.Lots() {
		if lot.Held {
			t.Fatalf("inventory was held while awaiting review")
		}
	}
	var cp events.ContainmentActionProposed
	mustDecode(t, proposed[0], &cp)

	srv := httptest.NewServer(containmentapi.New(h.contain, nil).Routes())
	defer srv.Close()

	// The review queue is what the ops console reads.
	var queue struct {
		Items []containment.Action `json:"items"`
	}
	getJSON(t, srv.URL+"/v1/containment/actions?status=PENDING_REVIEW", &queue)
	if len(queue.Items) != 1 || queue.Items[0].ActionID != cp.ActionID {
		t.Fatalf("review queue did not surface the pending action: %+v", queue.Items)
	}

	// A reviewer narrows the hold to a single lot and confirms.
	resp := post(t, srv.URL+"/v1/containment/actions/"+cp.ActionID+"/confirm",
		map[string]any{"actor": "ops:dana", "note": "confirmed against the FDA notice", "lot_codes_override": []string{"8H-1132"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm returned %d", resp.StatusCode)
	}

	taken := h.bus.PublishedOfType(events.TypeContainmentTaken)
	if len(taken) != 1 {
		t.Fatalf("expected containment.action.taken.v1 after confirmation, got %d", len(taken))
	}
	var ct events.ContainmentActionTaken
	mustDecode(t, taken[0], &ct)
	if ct.Decision != events.DecisionHumanConfirmed || ct.Actor != "ops:dana" {
		t.Fatalf("expected a human-attributed decision, got %s by %s", ct.Decision, ct.Actor)
	}
	if ct.Results[0].UnitsHeld != 40 {
		t.Fatalf("reviewer narrowed the hold to 8H-1132 (40 units), got %d", ct.Results[0].UnitsHeld)
	}
	// The narrowed hold means the second affected lot is still on sale, so only
	// the order holding 8H-1132 gets rescued.
	if n := len(h.bus.PublishedOfType(events.TypeOrderRescueProposed)); n != 1 {
		t.Fatalf("expected 1 rescue after the narrowed hold, got %d", n)
	}
}

// TestRejectIsAuditedAndHoldsNothing covers the reviewer saying no.
func TestRejectIsAuditedAndHoldsNothing(t *testing.T) {
	cfg := containment.DefaultConfig()
	cfg.AutoHoldThreshold = 0.995
	h := newHarness(t, cfg)
	publishNotice(t, h)

	var cp events.ContainmentActionProposed
	mustDecode(t, h.bus.PublishedOfType(events.TypeContainmentProposed)[0], &cp)

	srv := httptest.NewServer(containmentapi.New(h.contain, nil).Routes())
	defer srv.Close()
	resp := post(t, srv.URL+"/v1/containment/actions/"+cp.ActionID+"/reject",
		map[string]string{"actor": "ops:dana", "reason": "notice covers a different pack size"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject returned %d", resp.StatusCode)
	}

	var ct events.ContainmentActionTaken
	mustDecode(t, h.bus.PublishedOfType(events.TypeContainmentTaken)[0], &ct)
	if ct.Decision != events.DecisionHumanRejected {
		t.Fatalf("expected HUMAN_REJECTED, got %s", ct.Decision)
	}
	for _, lot := range h.inventory.Lots() {
		if lot.Held {
			t.Fatalf("a rejected action must not hold inventory")
		}
	}
	if n := len(h.bus.PublishedOfType(events.TypeOrderRescueProposed)); n != 0 {
		t.Fatalf("a rejected action must not rescue orders, got %d", n)
	}
	// Repeating the decision is a conflict, not a second event.
	if resp := post(t, srv.URL+"/v1/containment/actions/"+cp.ActionID+"/reject",
		map[string]string{"actor": "ops:dana", "reason": "again"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second reject returned %d, want 409", resp.StatusCode)
	}
}

// TestThresholdTuningTakesEffectWithoutRedeploy covers the ops console config surface.
func TestThresholdTuningTakesEffectWithoutRedeploy(t *testing.T) {
	cfg := containment.DefaultConfig()
	cfg.AutoHoldThreshold = 0.995
	h := newHarness(t, cfg)

	srv := httptest.NewServer(containmentapi.New(h.contain, nil).Routes())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/containment/config",
		strings.NewReader(`{"auto_hold_threshold":0.80,"sku_scope_threshold":0.95,"actor":"ops:dana"}`))
	req.Header.Set("Content-Type", "application/json")
	put, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put config: %v", err)
	}
	defer put.Body.Close()
	if put.StatusCode != http.StatusOK {
		t.Fatalf("put config returned %d", put.StatusCode)
	}

	publishNotice(t, h)
	if n := len(h.bus.PublishedOfType(events.TypeContainmentTaken)); n != 1 {
		t.Fatalf("lowered threshold should auto-hold, got %d taken events", n)
	}
}

// TestRedeliveryIsIdempotent: RabbitMQ guarantees at-least-once, so a replay must
// not double-hold inventory or double-propose a rescue.
func TestRedeliveryIsIdempotent(t *testing.T) {
	h := newHarness(t, containment.DefaultConfig())
	publishNotice(t, h)
	first := len(h.bus.Published())

	// Same incident arriving again as a fresh delivery (new event id, same notice).
	publishNotice(t, h)

	if got := len(h.bus.PublishedOfType(events.TypeContainmentTaken)); got != 1 {
		t.Fatalf("redelivery produced %d containment actions, want 1", got)
	}
	if got := len(h.bus.PublishedOfType(events.TypeOrderRescueProposed)); got != 1 {
		t.Fatalf("redelivery produced %d rescues, want 1", got)
	}
	if len(h.bus.Published()) <= first {
		t.Fatalf("sanity: the second notice should still have been published")
	}
}

// TestConsentIsRequired: a substitute must never be applied without the
// customer's own click.
func TestConsentIsRequired(t *testing.T) {
	h := newHarness(t, containment.DefaultConfig())
	publishNotice(t, h)
	var rp events.OrderRescueProposed
	mustDecode(t, h.bus.PublishedOfType(events.TypeOrderRescueProposed)[0], &rp)

	srv := httptest.NewServer(rescueapi.New(h.rescue, nil).Routes())
	defer srv.Close()

	resp := post(t, srv.URL+"/v1/rescues/"+rp.RescueID+"/confirm",
		map[string]string{"option_id": "sub-00012345678905", "consent_token": "forged"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("forged consent token returned %d, want 403", resp.StatusCode)
	}
	if got := swappedTo(h, "ORD-1001", "li-1"); got != "" {
		t.Fatalf("order was swapped on a forged token")
	}

	token := h.rescue.ConsentToken(rp.RescueID)
	if resp := post(t, srv.URL+"/v1/rescues/"+rp.RescueID+"/confirm",
		map[string]string{"option_id": "sub-does-not-exist", "consent_token": token}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown option returned %d, want 400", resp.StatusCode)
	}
	if resp := post(t, srv.URL+"/v1/rescues/"+rp.RescueID+"/confirm",
		map[string]string{"option_id": "cancel", "consent_token": token}); resp.StatusCode != http.StatusOK {
		t.Fatalf("valid cancel returned %d", resp.StatusCode)
	}
	// Second confirmation of the same rescue is a conflict.
	if resp := post(t, srv.URL+"/v1/rescues/"+rp.RescueID+"/confirm",
		map[string]string{"option_id": "cancel", "consent_token": token}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("double confirmation returned %d, want 409", resp.StatusCode)
	}
}

// TestPlatformFailureIsReportedNotSwallowed.
func TestPlatformFailureIsReportedNotSwallowed(t *testing.T) {
	h := newHarness(t, containment.DefaultConfig())
	h.inventory.FailFor["00041196910537"] = errShopifyDown{}
	publishNotice(t, h)

	var ct events.ContainmentActionTaken
	mustDecode(t, h.bus.PublishedOfType(events.TypeContainmentTaken)[0], &ct)
	if ct.Results[0].Status != "FAILED" || ct.Results[0].Error == "" {
		t.Fatalf("expected a FAILED result carrying the platform error, got %+v", ct.Results[0])
	}
	if n := len(h.bus.PublishedOfType(events.TypeOrderRescueProposed)); n != 0 {
		t.Fatalf("nothing was held, so no order should be rescued, got %d", n)
	}
}

type errShopifyDown struct{}

func (errShopifyDown) Error() string { return "shopify: 503 service unavailable" }

// --------------------------------------------------------------------- helpers

func mustDecode(t *testing.T, env events.Envelope, dst any) {
	t.Helper()
	if err := env.Validate(); err != nil {
		t.Fatalf("envelope %s failed contract validation: %v", env.EventType, err)
	}
	if err := env.Into(dst); err != nil {
		t.Fatalf("decode %s: %v", env.EventType, err)
	}
}

func post(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func getJSON(t *testing.T, url string, dst any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func hasOption(options []events.RescueOption, id string) bool {
	for _, o := range options {
		if o.OptionID == id {
			return true
		}
	}
	return false
}

func swappedTo(h *harness, orderID, lineID string) string {
	for _, o := range h.orders.Orders() {
		if o.OrderID != orderID {
			continue
		}
		for _, li := range o.LineItems {
			if li.LineItemID == lineID {
				return li.SwappedTo
			}
		}
	}
	return ""
}

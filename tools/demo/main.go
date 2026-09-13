// Command demo runs the whole core domain layer in one process: one in-memory
// bus, all three services, their three HTTP APIs, and a seeded in-memory shop
// driven through the real libs/shopify/hold adapter.
//
// It exists because the full deployment needs a RabbitMQ broker; this harness
// exercises the identical service code and event flow without one, so the
// storefront can develop against live APIs. It is a demo harness, not a
// deployment target: state is in memory and dies with the process.
//
// Ports: 8081 resolution, 8082 containment, 8083 order rescue, 8085 audit,
// 8090 harness.
//
//	POST /demo/notice   inject a recall.raw.received.v1 message (flat ingestion shape)
//	GET  /demo/state    inventory, events and rescues, for verification
//	GET  /demo/events   every envelope published so far
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"soteria/libs/core/bus"
	"soteria/libs/core/events"
	"soteria/libs/core/offacts"
	"soteria/libs/shopify"
	"soteria/libs/shopify/fake"
	"soteria/libs/shopify/hold"

	auditapi "soteria/services/audit-proof-service/api"
	"soteria/services/audit-proof-service/audit"
	containmentapi "soteria/services/containment-service/api"
	"soteria/services/containment-service/containment"
	rescueapi "soteria/services/order-rescue-service/api"
	"soteria/services/order-rescue-service/orders"
	"soteria/services/order-rescue-service/rescue"
	resolutionapi "soteria/services/resolution-service/api"
	"soteria/services/resolution-service/catalog"
	"soteria/services/resolution-service/resolver"
)

const (
	sellingLoc = "gid://shopify/Location/1"
	quarantLoc = "gid://shopify/Location/2"
	consentKey = "demo-consent-secret"
)

type harness struct {
	bus       *bus.InMem
	audit     *audit.Service
	shop      *fake.Store
	resolve   *resolver.Resolver
	resStore  *resolver.Store
	contain   *containment.Service
	rescue    *rescue.Service
	orders    *orders.Memory
	logger    *slog.Logger
	startedAt time.Time
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	h := build(logger)

	servers := []*http.Server{
		serve(":8081", resolutionapi.New(h.resStore, nil).Routes(), "resolution-service", logger),
		serve(":8082", containmentapi.New(h.contain, nil).Routes(), "containment-service", logger),
		serve(":8083", rescueapi.New(h.rescue, nil).Routes(), "order-rescue-service", logger),
		serve(":8085", auditapi.New(h.audit).Routes(), "audit-proof-service", logger),
		serve(":8090", h.routes(), "demo-harness", logger),
	}

	logger.Info("Soteria demo running",
		"resolution", "http://localhost:8081", "containment", "http://localhost:8082",
		"rescue", "http://localhost:8083", "audit", "http://localhost:8085",
		"harness", "http://localhost:8090")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdown)
	}
	logger.Info("stopped")
}

// build wires the three services exactly as their own main functions do, onto one bus.
func build(logger *slog.Logger) *harness {
	b := bus.NewInMem(logger)
	shop := seedShop()

	selling, quarantine, err := hold.ResolveLocations(context.Background(), shop, "Quarantine")
	if err != nil {
		logger.Error("cannot resolve shop locations", "err", err)
		os.Exit(1)
	}

	cat := catalog.NewStore(shop, time.Second, logger)
	resStore := resolver.NewStore()
	resStore.SetLedger(func(ctx context.Context, gtin string) []string {
		p, ok := cat.ByGTIN(ctx, gtin)
		if !ok {
			return nil
		}
		return p.LotCodes
	})
	res := resolver.New(cat, b, resStore, logger)
	con := containment.New(containment.NewStore(containment.DefaultConfig()), hold.New(shop, selling, quarantine), b, logger)

	ord := seedOrders()
	resc := rescue.New(ord, seedSubstitutes(), seedAllergens(), b, consentKey, logger)

	// No timestamper in the demo: anchoring every run against a public authority
	// would be rude to it, and the dossier says plainly when it is unanchored.
	aud := audit.New(b, nil, logger)

	for _, register := range []func(bus.Consumer) error{res.Register, con.Register, resc.Register, aud.Register} {
		if err := register(b); err != nil {
			logger.Error("cannot subscribe", "err", err)
			os.Exit(1)
		}
	}
	return &harness{
		bus: b, shop: shop, resolve: res, resStore: resStore, audit: aud,
		contain: con, rescue: resc, orders: ord, logger: logger, startedAt: time.Now(),
	}
}

// ---------------------------------------------------------------- seed data

func seedShop() *fake.Store {
	s := fake.New()
	s.AddLocation(shopify.Location{ID: sellingLoc, Name: "Main Warehouse", IsActive: true, Fulfills: true})
	s.AddLocation(shopify.Location{ID: quarantLoc, Name: "Quarantine", IsActive: true})
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/1", ProductID: "gid://shopify/Product/1",
		ProductStatus: shopify.StatusActive, ProductTitle: "Sunfield Farms Chewy Granola Bars",
		Title: "12 ct", Vendor: "Sunfield Farms", SKU: "SF-GB-12", Barcode: "041196910537",
		Price: "4.49", InventoryItemID: "gid://shopify/InventoryItem/1",
		Inventory: []shopify.InventoryLevel{{LocationID: sellingLoc, Available: 125}},
		Lots: []shopify.Lot{
			{Code: "8H-1132", Units: 40},
			{Code: "8H-1133", Units: 25},
			{Code: "8H-2000", Units: 60},
		},
	})
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/2", ProductID: "gid://shopify/Product/2",
		ProductStatus: shopify.StatusActive, ProductTitle: "Harvest Lane Oat and Honey Granola Bars",
		Title: "12 ct", Vendor: "Harvest Lane", SKU: "HL-GB-12", Barcode: "012345678905",
		Price: "4.49", InventoryItemID: "gid://shopify/InventoryItem/2",
		Inventory: []shopify.InventoryLevel{{LocationID: sellingLoc, Available: 120}},
		Lots:      []shopify.Lot{{Code: "K221", Units: 120}},
	})
	s.AddVariant(shopify.Variant{
		ID: "gid://shopify/ProductVariant/3", ProductID: "gid://shopify/Product/3",
		ProductStatus: shopify.StatusActive, ProductTitle: "Nutty Trail Peanut Crunch Granola Bars",
		Title: "12 ct", Vendor: "Nutty Trail", SKU: "NT-GB-12", Barcode: "073123456788",
		Price: "4.49", InventoryItemID: "gid://shopify/InventoryItem/3",
		Inventory: []shopify.InventoryLevel{{LocationID: sellingLoc, Available: 80}},
		Lots:      []shopify.Lot{{Code: "P908", Units: 80}},
	})
	return s
}

func seedOrders() *orders.Memory {
	price := events.Money{AmountMinor: 449, Currency: "USD"}
	line := func(id, lot string, qty int) orders.LineItem {
		return orders.LineItem{
			LineItemID: id, GTIN: "00041196910537", SKU: "SF-GB-12",
			ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
			LotCode:      lot, Quantity: qty, UnitPrice: price,
		}
	}
	return orders.NewMemory(
		orders.Order{
			OrderID: "ORD-1001", Status: orders.StatusPending,
			Customer:  events.Customer{CustomerID: "cust-77", Email: "buyer@example.com", Locale: "en-US"},
			LineItems: []orders.LineItem{line("li-1", "8H-1132", 2)},
		},
		orders.Order{
			OrderID: "ORD-1002", Status: orders.StatusPending,
			Customer:  events.Customer{CustomerID: "cust-91", Email: "clean@example.com", Locale: "en-US"},
			LineItems: []orders.LineItem{line("li-2", "8H-2000", 1)},
		},
		orders.Order{
			OrderID: "ORD-1003", Status: orders.StatusShipped,
			Customer:  events.Customer{CustomerID: "cust-12", Email: "shipped@example.com", Locale: "en-US"},
			LineItems: []orders.LineItem{line("li-3", "8H-1133", 3)},
		},
	)
}

func seedSubstitutes() rescue.SubstituteSource {
	price := events.Money{AmountMinor: 449, Currency: "USD"}
	return rescue.NewMemoryCatalog(
		rescue.Substitute{GTIN: "00041196910537", SKU: "SF-GB-12", ProductTitle: "Sunfield Farms Chewy Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: price, InStock: 0},
		rescue.Substitute{GTIN: "00012345678905", SKU: "HL-GB-12", ProductTitle: "Harvest Lane Oat and Honey Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: price, InStock: 120},
		rescue.Substitute{GTIN: "00073123456788", SKU: "NT-GB-12", ProductTitle: "Nutty Trail Peanut Crunch Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: price, InStock: 80},
		rescue.Substitute{GTIN: "00099999999999", SKU: "MY-GB-12", ProductTitle: "Mystery Snack Bars 12 ct",
			Category: "granola-bars", UnitPrice: price, InStock: 40},
		rescue.Substitute{GTIN: "00088888888880", SKU: "PR-GB-12", ProductTitle: "Premium Seed Granola Bars 12 ct",
			Category: "granola-bars", UnitPrice: events.Money{AmountMinor: 699, Currency: "USD"}, InStock: 30},
	)
}

// seedAllergens covers each coverage case the storefront contract distinguishes:
// a compatible substitute, one carrying the hazard, and one nobody has annotated.
func seedAllergens() offacts.Provider {
	return offacts.NewStatic(
		offacts.Product{GTIN: "00041196910537", Name: "Sunfield Farms Chewy Granola Bars",
			Allergens: []string{"en:gluten"}, Coverage: offacts.CoverageComplete},
		offacts.Product{GTIN: "00012345678905", Name: "Harvest Lane Oat and Honey Granola Bars",
			Allergens: []string{"en:gluten"}, Coverage: offacts.CoverageComplete},
		offacts.Product{GTIN: "00073123456788", Name: "Nutty Trail Peanut Crunch Granola Bars",
			Allergens: []string{"en:gluten", "en:peanuts"}, Coverage: offacts.CoverageComplete},
		offacts.Product{GTIN: "00099999999999", Name: "Mystery Snack Bars", Coverage: offacts.CoveragePartial},
		offacts.Product{GTIN: "00088888888880", Name: "Premium Seed Granola Bars",
			Allergens: []string{"en:gluten"}, Coverage: offacts.CoverageComplete},
	)
}

// ---------------------------------------------------------------- harness API

func (h *harness) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "demo-harness",
			"uptime": time.Since(h.startedAt).Truncate(time.Second).String()})
	})
	mux.HandleFunc("POST /demo/notice", h.injectNotice)
	mux.HandleFunc("GET /demo/state", h.state)
	mux.HandleFunc("GET /demo/events", h.eventLog)
	return mux
}

// injectNotice publishes a recall.raw.received.v1 message, standing in for the
// ingestion services. The body is the flat ingestion contract, so a payload
// captured from a live feed can be replayed verbatim.
func (h *harness) injectNotice(w http.ResponseWriter, r *http.Request) {
	body, err := readAll(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	env, err := events.InboundEnvelope(events.TypeRecallRawReceived, body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.bus.Publish(r.Context(), env); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"published":    env.EventType,
		"event_id":     env.EventID,
		"resolved":     len(h.bus.PublishedOfType(events.TypeLotResolved)),
		"contained":    len(h.bus.PublishedOfType(events.TypeContainmentTaken)),
		"rescues":      len(h.bus.PublishedOfType(events.TypeOrderRescueProposed)),
		"dead_letters": len(h.bus.DeadLetters()),
	})
}

func (h *harness) state(w http.ResponseWriter, r *http.Request) {
	type lotView struct {
		GTIN       string `json:"gtin"`
		Lot        string `json:"lot_code"`
		Units      int    `json:"units"`
		Held       bool   `json:"held"`
		Sellable   int    `json:"sellable_at_selling_location"`
		Quarantine int    `json:"in_quarantine"`
	}
	var inventory []lotView
	variants, _ := h.shop.Catalog(r.Context())
	for _, v := range variants {
		for _, l := range v.Lots {
			inventory = append(inventory, lotView{
				GTIN: v.Barcode, Lot: l.Code, Units: l.Units, Held: l.Held,
				Sellable: v.AvailableAt(sellingLoc), Quarantine: v.AvailableAt(quarantLoc),
			})
		}
	}

	rescues := h.rescue.List("", "", "")
	tokens := map[string]string{}
	for _, rec := range rescues {
		tokens[rec.RescueID] = h.rescue.ConsentToken(rec.RescueID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"inventory":           inventory,
		"orders":              h.orders.Orders(),
		"resolutions":         h.resStore.List("", "", 20),
		"containment_actions": h.contain.Store().List("", "", 20),
		"rescues":             rescues,
		"consent_tokens":      tokens,
		"events_published":    len(h.bus.Published()),
		"dead_letters":        h.bus.DeadLetters(),
	})
}

func (h *harness) eventLog(w http.ResponseWriter, r *http.Request) {
	type row struct {
		Type     string          `json:"event_type"`
		Producer string          `json:"producer"`
		Incident string          `json:"incident_id,omitempty"`
		At       time.Time       `json:"occurred_at"`
		Payload  json.RawMessage `json:"payload"`
	}
	var out []row
	for _, e := range h.bus.Published() {
		out = append(out, row{Type: e.EventType, Producer: e.Producer, Incident: e.CorrelationID, At: e.OccurredAt, Payload: e.Payload})
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "events": out})
}

// ---------------------------------------------------------------- plumbing

func serve(addr string, handler http.Handler, name string, logger *slog.Logger) *http.Server {
	srv := &http.Server{Addr: addr, Handler: cors(handler), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "service", name, "addr", addr, "err", err)
		}
	}()
	return srv
}

// cors lets the storefront dev server call these APIs from another origin. The
// real deployment fronts them behind one gateway and does not need this.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("body must be a JSON object: %w", err)
	}
	return raw, nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}

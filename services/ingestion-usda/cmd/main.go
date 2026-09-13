// ingestion-usda polls the USDA FSIS recall API (meat, poultry, egg products)
// and publishes recall.raw.received.v1 events to ingestion.x.
package main

import (
	"soteria/libs/feedkit/app"
	"soteria/services/ingestion-usda/internal/fsis"
)

func main() {
	app.Main("ingestion-usda", func(cfg app.Config) ([]app.SourceSpec, error) {
		src := fsis.New(cfg.HTTP)
		if u := cfg.Env("FSIS_BASE_URL", ""); u != "" {
			src.BaseURL = u // e.g. a US-egress proxy or a local fixture server
		}
		return []app.SourceSpec{{Source: src}}, nil
	})
}

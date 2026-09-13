// ingestion-fda polls openFDA food enforcement reports and the FDA recalls
// press RSS, publishing recall.raw.received.v1 events to ingestion.x.
package main

import (
	"soteria/libs/feedkit/app"
	"soteria/services/ingestion-fda/internal/fdarss"
	"soteria/services/ingestion-fda/internal/openfda"
)

func main() {
	app.Main("ingestion-fda", func(cfg app.Config) ([]app.SourceSpec, error) {
		return []app.SourceSpec{
			{Source: openfda.New(cfg.HTTP, cfg.Env("OPENFDA_API_KEY", ""))},
			{Source: fdarss.New(cfg.HTTP)},
		}, nil
	})
}

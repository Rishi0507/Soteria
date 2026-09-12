// ingestion-rasff ingests EU RASFF notifications and publishes
// recall.raw.received.v1 events to ingestion.x.
//
// RASFF_MODE=file (default) reads JSON/CSV exports from RASFF_FILE_DIR.
// RASFF_MODE=http is reserved for a verified portal adapter.
package main

import (
	"fmt"

	"soteria/libs/feedkit/app"
	rasffile "soteria/services/ingestion-rasff/internal/rasff/file"
	rasffhttp "soteria/services/ingestion-rasff/internal/rasff/http"
)

func main() {
	app.Main("ingestion-rasff", func(cfg app.Config) ([]app.SourceSpec, error) {
		switch mode := cfg.Env("RASFF_MODE", "file"); mode {
		case "file":
			dir := cfg.Env("RASFF_FILE_DIR", "fixtures")
			cfg.Log.Info("rasff file mode", "dir", dir)
			return []app.SourceSpec{{Source: rasffile.New(dir)}}, nil
		case "http":
			src, err := rasffhttp.New(cfg.Env("RASFF_BASE_URL", "https://webgate.ec.europa.eu/rasff-window/backend"))
			if err != nil {
				return nil, err
			}
			return []app.SourceSpec{{Source: src}}, nil
		default:
			return nil, fmt.Errorf("RASFF_MODE=%q: want file or http", mode)
		}
	})
}

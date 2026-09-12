
.PHONY: help test replay once run docker

help:
	@echo "test    pytest"
	@echo "replay  diff the LesserEvil fixtures (dry run)"
	@echo "once    live snapshot+diff of every source"
	@echo "run     poll hourly, /healthz on :8085"
	@echo "docker  build image from repo root"

test:
	python -m pytest -q

replay:
	python -m silentdiff replay fixtures/lesserevil_before.json fixtures/lesserevil_after.json --source shopify:lesserevil.com --dry-run

once:
	python -m silentdiff once

run:
	python -m silentdiff run

docker:
	cd ../.. && docker build -f services/ingestion-silent-diff/Dockerfile -t soteria/ingestion-silent-diff .

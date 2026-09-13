.PHONY: help test replay once run docker

help:
	@echo "test    pytest"
	@echo "replay  judge the fixture listings against the fixture watch-list (dry run; LLM if GROQ_API_KEY set)"
	@echo "once    one pass over sources.yaml"
	@echo "run     poll + consume containment events; /healthz on :8087"
	@echo "docker  build image from repo root"

test:
	python -m pytest -q

replay:
	python -m antievasion replay fixtures/listings/liquidation_2026-09-12.json --dry-run

once:
	python -m antievasion once

run:
	python -m antievasion run

docker:
	cd ../.. && docker build -f services/anti-evasion-service/Dockerfile -t soteria/anti-evasion-service .

# recall-extractor — Go must be on PATH. GROQ_API_KEY from .env (repo root or here).
BIN := bin/recall-extractor
F ?= testdata/raw.jsonl
T ?=

.PHONY: help build vet test eval eval-record run replay text docker clean

help:
	@echo "test         unit + contract + eval (recorded answers; no key needed)"
	@echo "eval         print the per-case eval table"
	@echo "eval-record  re-run the eval live against Groq and rewrite eval/recordings.json"
	@echo "replay F=..  extract a JSON/JSONL file of recall.raw.received.v1 events"
	@echo "text T=..    extract an ad-hoc notice text"
	@echo "run          consume from RabbitMQ; HTTP on PORT (8086)"

build:
	go build -o $(BIN) ./cmd/recall-extractor

vet:
	go vet ./...

test: vet
	go test ./...

eval:
	go test ./eval -run TestExtractionSuite -v -count=1 2>&1 | grep -E "eval_test.go" | sed "s/^ *eval_test.go:[0-9]*: //"

eval-record:
	EVAL_RECORD=1 go test ./eval -run TestExtractionSuite -v -count=1 2>&1 | grep -E "eval_test.go|recorded" | sed "s/^ *eval_test.go:[0-9]*: //"

run:
	go run ./cmd/recall-extractor

replay:
	go run ./cmd/recall-extractor --replay $(F)

text:
	go run ./cmd/recall-extractor --text "$(T)"

docker:
	cd ../.. && docker build -f services/recall-extractor/Dockerfile -t soteria/recall-extractor .

clean:
	rm -rf bin data

# ingestion-fda — run `make help` for targets. Go must be on PATH.
SVC := ingestion-fda
BIN := bin/$(SVC)
ARGS ?=

.PHONY: help build test vet run once dry-run docker clean

help:
	@echo "build    compile to $(BIN)"
	@echo "test     go vet + go test (includes the contract test)"
	@echo "run      long-running poller with /healthz on $$HTTP_ADDR (default :8080)"
	@echo "once     single poll of every source, publish to RabbitMQ, exit"
	@echo "dry-run  single poll, print events as JSON lines, publish nothing"
	@echo "docker   build the container image from the repo root"

build:
	go build -o $(BIN) ./cmd

vet:
	go vet ./...

test: vet
	go test ./...

run:
	go run ./cmd $(ARGS)

once:
	go run ./cmd --once $(ARGS)

dry-run:
	go run ./cmd --once --dry-run $(ARGS)

docker:
	cd ../.. && docker build -f services/$(SVC)/Dockerfile -t soteria/$(SVC) .

clean:
	rm -rf bin data

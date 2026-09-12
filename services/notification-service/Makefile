# notification-service — Go must be on PATH.
BIN := bin/notification-service
ARGS ?=

.PHONY: help build vet test run replay docker clean

help:
	@echo "build   compile to $(BIN)"
	@echo "test    go vet + go test (includes contract tests)"
	@echo "run     consume from RabbitMQ (RABBITMQ_URL) and serve /healthz on PORT (8084)"
	@echo "replay  push testdata/sample_events.json through the bus and deliver (real Slack/email if keys are set)"
	@echo "docker  build the container image from the repo root"

build:
	go build -o $(BIN) ./cmd/notification-service

vet:
	go vet ./...

test: vet
	go test ./...

run:
	go run ./cmd/notification-service $(ARGS)

replay:
	go run ./cmd/notification-service --replay testdata/sample_events.json $(ARGS)

docker:
	cd ../.. && docker build -f services/notification-service/Dockerfile -t soteria/notification-service .

clean:
	rm -rf bin data

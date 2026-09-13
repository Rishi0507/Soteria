# Build from the repo root:
#   docker build -f services/containment-service/Dockerfile -t soteria/containment-service .
#
# Built standalone (GOWORK=off) through this service's own replace
# directives, so adding modules to go.work can never break this image.
FROM golang:1.26-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/services/containment-service
COPY libs/core/go.mod libs/core/go.sum /src/libs/core/
COPY libs/shopify/go.mod libs/shopify/go.sum /src/libs/shopify/
COPY services/containment-service/go.mod services/containment-service/go.sum ./
RUN go mod download
COPY libs/core/ /src/libs/core/
COPY libs/shopify/ /src/libs/shopify/
COPY services/containment-service/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/containment-service ./cmd/containment-service

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/containment-service /app/containment-service
# Fixtures, used only when no store is configured. The service logs loudly
# when it falls back to them, so an image running on fixtures is never silent.
COPY services/containment-service/testdata/ /app/testdata/
ENV INVENTORY_PATH=/app/testdata/inventory.json \
    PORT=8082
EXPOSE 8082
ENTRYPOINT ["/app/containment-service"]

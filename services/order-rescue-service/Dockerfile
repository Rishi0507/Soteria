# Build from the repo root:
#   docker build -f services/order-rescue-service/Dockerfile -t soteria/order-rescue-service .
#
# Built standalone (GOWORK=off) through this service's own replace
# directives, so adding modules to go.work can never break this image.
FROM golang:1.26-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/services/order-rescue-service
COPY libs/core/go.mod libs/core/go.sum /src/libs/core/
COPY libs/shopify/go.mod libs/shopify/go.sum /src/libs/shopify/
COPY services/order-rescue-service/go.mod services/order-rescue-service/go.sum ./
RUN go mod download
COPY libs/core/ /src/libs/core/
COPY libs/shopify/ /src/libs/shopify/
COPY services/order-rescue-service/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/order-rescue-service ./cmd/order-rescue-service

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/order-rescue-service /app/order-rescue-service
# Fixtures, used only when no store is configured. The service logs loudly
# when it falls back to them, so an image running on fixtures is never silent.
COPY services/order-rescue-service/testdata/ /app/testdata/
ENV ORDERS_PATH=/app/testdata/orders.json \
    SUBSTITUTES_PATH=/app/testdata/substitutes.json \
    ALLERGENS_PATH=/app/testdata/allergens.json \
    PORT=8083
EXPOSE 8083
ENTRYPOINT ["/app/order-rescue-service"]

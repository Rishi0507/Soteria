# Build from the repo root:
#   docker build -f services/resolution-service/Dockerfile -t soteria/resolution-service .
#
# Built standalone (GOWORK=off) through this service's own replace
# directives, so adding modules to go.work can never break this image.
FROM golang:1.26-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/services/resolution-service
COPY libs/core/go.mod libs/core/go.sum /src/libs/core/
COPY libs/shopify/go.mod libs/shopify/go.sum /src/libs/shopify/
COPY services/resolution-service/go.mod services/resolution-service/go.sum ./
RUN go mod download
COPY libs/core/ /src/libs/core/
COPY libs/shopify/ /src/libs/shopify/
COPY services/resolution-service/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/resolution-service ./cmd/resolution-service

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/resolution-service /app/resolution-service
# Fixtures, used only when no store is configured. The service logs loudly
# when it falls back to them, so an image running on fixtures is never silent.
COPY services/resolution-service/testdata/ /app/testdata/
ENV CATALOG_PATH=/app/testdata/catalog.json \
    PORT=8081
EXPOSE 8081
ENTRYPOINT ["/app/resolution-service"]

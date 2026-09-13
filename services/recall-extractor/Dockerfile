# docker build -f services/recall-extractor/Dockerfile -t soteria/recall-extractor .
# Standalone build (GOWORK=off) via the replace directives in go.mod.
FROM golang:1.26-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/services/recall-extractor
COPY libs/core/go.mod libs/core/go.sum /src/libs/core/
COPY libs/feedkit/go.mod libs/feedkit/go.sum /src/libs/feedkit/
COPY services/recall-extractor/go.mod services/recall-extractor/go.sum ./
RUN go mod download
COPY libs/core/ /src/libs/core/
COPY libs/feedkit/ /src/libs/feedkit/
COPY services/recall-extractor/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/recall-extractor ./cmd/recall-extractor && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/recall-extractor /app/recall-extractor
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY services/recall-extractor/eval/cases.json /app/eval/cases.json
ENV CACHE_PATH=/data/extract-cache.db PORT=8086
VOLUME ["/data"]
EXPOSE 8086
ENTRYPOINT ["/app/recall-extractor"]

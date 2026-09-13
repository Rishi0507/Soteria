# Build from the repo root:
#   docker build -f services/ingestion-rasff/Dockerfile -t soteria/ingestion-rasff .
#
# The service is built standalone (GOWORK=off) via the replace directive in
# its own go.mod, so the image only needs this service and libs/feedkit. It
# does NOT depend on go.work, so adding modules to the workspace can never
# break this build again.
FROM golang:1.26-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/services/ingestion-rasff
COPY libs/feedkit/go.mod libs/feedkit/go.sum /src/libs/feedkit/
COPY services/ingestion-rasff/go.mod services/ingestion-rasff/go.sum ./
RUN go mod download
COPY libs/feedkit/ /src/libs/feedkit/
COPY services/ingestion-rasff/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/ingestion-rasff ./cmd && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ingestion-rasff /app/ingestion-rasff
# distroless has no shell: pre-create the state dir owned by the runtime user
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY services/ingestion-rasff/fixtures /app/fixtures
ENV DB_PATH=/data/ingestion-rasff.db HTTP_ADDR=:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/ingestion-rasff"]

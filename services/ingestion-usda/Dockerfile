# Build from the repo root so the go.work workspace resolves libs/feedkit:
#   docker build -f services/ingestion-usda/Dockerfile -t soteria/ingestion-usda .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.work ./
COPY libs/feedkit/go.mod libs/feedkit/go.sum libs/feedkit/
COPY services/ingestion-fda/go.mod services/ingestion-fda/go.sum services/ingestion-fda/
COPY services/ingestion-usda/go.mod services/ingestion-usda/go.sum services/ingestion-usda/
COPY services/ingestion-rasff/go.mod services/ingestion-rasff/go.sum services/ingestion-rasff/
# every module listed in go.work must be present before go mod download
COPY libs/core/go.mod libs/core/go.sum libs/core/
COPY services/resolution-service/go.mod services/resolution-service/go.sum services/resolution-service/
COPY services/containment-service/go.mod services/containment-service/go.sum services/containment-service/
COPY services/order-rescue-service/go.mod services/order-rescue-service/go.sum services/order-rescue-service/
COPY tests/e2e/go.mod tests/e2e/go.sum tests/e2e/
RUN go mod download
COPY libs/ libs/
COPY services/ services/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ingestion-usda ./services/ingestion-usda/cmd && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ingestion-usda /app/ingestion-usda
# distroless has no shell: pre-create the state dir owned by the runtime user
COPY --from=build --chown=nonroot:nonroot /out/data /data

ENV DB_PATH=/data/ingestion-usda.db HTTP_ADDR=:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/ingestion-usda"]

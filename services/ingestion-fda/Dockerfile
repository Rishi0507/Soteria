# Build from the repo root so the go.work workspace resolves libs/feedkit:
#   docker build -f services/ingestion-fda/Dockerfile -t soteria/ingestion-fda .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.work ./
COPY libs/feedkit/go.mod libs/feedkit/go.sum libs/feedkit/
COPY services/ingestion-fda/go.mod services/ingestion-fda/go.sum services/ingestion-fda/
COPY services/ingestion-usda/go.mod services/ingestion-usda/go.sum services/ingestion-usda/
COPY services/ingestion-rasff/go.mod services/ingestion-rasff/go.sum services/ingestion-rasff/
RUN go mod download
COPY libs/ libs/
COPY services/ services/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ingestion-fda ./services/ingestion-fda/cmd && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/ingestion-fda /app/ingestion-fda
# distroless has no shell: pre-create the state dir owned by the runtime user
COPY --from=build --chown=nonroot:nonroot /out/data /data

ENV DB_PATH=/data/ingestion-fda.db HTTP_ADDR=:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/ingestion-fda"]

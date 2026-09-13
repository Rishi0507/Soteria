# Build from the repo root:
#   docker build -f services/audit-proof-service/Dockerfile -t soteria/audit-proof-service .
#
# Built standalone (GOWORK=off) through this service's own replace
# directives, so adding modules to go.work can never break this image.
FROM golang:1.26-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/services/audit-proof-service
COPY libs/core/go.mod libs/core/go.sum /src/libs/core/
COPY libs/feedkit/go.mod libs/feedkit/go.sum /src/libs/feedkit/
COPY services/audit-proof-service/go.mod services/audit-proof-service/go.sum ./
RUN go mod download
COPY libs/core/ /src/libs/core/
COPY libs/feedkit/ /src/libs/feedkit/
COPY services/audit-proof-service/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/audit-proof-service ./cmd/audit-proof-service

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/audit-proof-service /app/audit-proof-service
ENV PORT=8084
EXPOSE 8084
ENTRYPOINT ["/app/audit-proof-service"]

# Build from the repo root:
#   docker build -f services/notification-service/Dockerfile -t soteria/notification-service .
# Standalone build (GOWORK=off) via the replace directives in go.mod; only this
# service plus libs/core and libs/feedkit are copied, so go.work growth never
# breaks the image.
FROM golang:1.26-alpine AS build
ENV GOWORK=off CGO_ENABLED=0
WORKDIR /src/services/notification-service
COPY libs/core/go.mod libs/core/go.sum /src/libs/core/
COPY libs/feedkit/go.mod libs/feedkit/go.sum /src/libs/feedkit/
COPY services/notification-service/go.mod services/notification-service/go.sum ./
RUN go mod download
COPY libs/core/ /src/libs/core/
COPY libs/feedkit/ /src/libs/feedkit/
COPY services/notification-service/ ./
RUN go build -trimpath -ldflags="-s -w" -o /out/notification-service ./cmd/notification-service && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/notification-service /app/notification-service
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY services/notification-service/testdata /app/testdata
ENV DB_PATH=/data/notification.db PORT=8084
VOLUME ["/data"]
EXPOSE 8084
ENTRYPOINT ["/app/notification-service"]

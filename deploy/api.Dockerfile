FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/assay ./cmd/assay

FROM alpine:3.22
RUN addgroup -S assay && adduser -S assay -G assay && mkdir /data && chown assay:assay /data
COPY --from=build /out/assay /assay
USER assay
EXPOSE 8080
ENTRYPOINT ["/assay"]
CMD ["-addr", "0.0.0.0:8080", "-cache-dir", "/data/cache", "-cache-max-mb", "10240", "-history-dir", "/data/history", "-event-dir", "/data/events"]

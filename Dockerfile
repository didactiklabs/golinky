FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git gcc musl-dev
WORKDIR /build
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o golinky -ldflags="-s -w" .

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/golinky .
RUN mkdir -p /app/data
EXPOSE 8080
RUN addgroup -g 1000 golinky && \
    adduser -D -u 1000 -G golinky golinky && \
    chown -R golinky:golinky /app
USER golinky

ENTRYPOINT ["/app/golinky"]
CMD ["-listen", "0.0.0.0:8080", "-sqlitedb", "/app/data/golinky.db"]

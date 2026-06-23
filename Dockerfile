FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/payment-webhook-processor ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates wget

WORKDIR /app

COPY --from=builder /out/payment-webhook-processor /usr/local/bin/payment-webhook-processor

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/payment-webhook-processor"]

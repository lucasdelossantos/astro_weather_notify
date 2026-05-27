FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /astro-notify ./cmd/notify

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /astro-notify /usr/local/bin/astro-notify

CMD ["/usr/local/bin/astro-notify"]

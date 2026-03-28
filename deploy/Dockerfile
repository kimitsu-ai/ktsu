FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o ktsu ./cmd/ktsu

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/ktsu /usr/local/bin/ktsu
ENTRYPOINT ["ktsu"]

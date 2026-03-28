# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY ./ .
RUN CGO_ENABLED=0 GOOS=linux go build -o ktsu ./cmd/ktsu

# Final stage
FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/ktsu /usr/local/bin/ktsu
ENTRYPOINT ["ktsu"]

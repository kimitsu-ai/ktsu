FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o kimitsu ./cmd/kimitsu

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/kimitsu /usr/local/bin/kimitsu
ENTRYPOINT ["kimitsu"]

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY ktsu /usr/local/bin/ktsu
ENTRYPOINT ["ktsu"]

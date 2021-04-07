# syntax=docker/dockerfile:experimental
FROM golang:alpine

WORKDIR /build
ADD . .

RUN --mount=type=cache,target=/go/pkg/mod \
	  --mount=type=cache,target=/root/.cache/go-build \
      go build -o /exporter ./cmd/exporter

FROM alpine
LABEL source_repository=https://github.com/sapcc/http-keep-alive-monitor
COPY --from=0 /exporter /exporter
ENTRYPOINT ["/exporter"]

FROM golang:1.22.2-alpine3.19 as build

WORKDIR /build
ADD . .

RUN go build -o /exporter ./cmd/exporter

FROM alpine:3.19
LABEL source_repository=https://github.com/sapcc/http-keep-alive-monitor
COPY --from=build /exporter /exporter
ENTRYPOINT ["/exporter"]

FROM golang:1.22.3-alpine3.19 as build

WORKDIR /build
ADD . .

RUN go build -o /exporter ./cmd/exporter

FROM gcr.io/distroless/static:nonroot
LABEL source_repository=https://github.com/sapcc/http-keep-alive-monitor
COPY --from=build /exporter /exporter
ENTRYPOINT ["/exporter"]

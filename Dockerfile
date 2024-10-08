FROM golang:1.23-alpine as build

WORKDIR /build
ADD . .

RUN go build -o /exporter ./cmd/exporter

FROM gcr.io/distroless/static:nonroot
LABEL source_repository=https://github.com/sapcc/http-keep-alive-monitor
COPY --from=build /exporter /exporter
ENTRYPOINT ["/exporter"]

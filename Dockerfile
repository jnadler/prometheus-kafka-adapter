FROM golang:1.16.3-buster as build

WORKDIR /src/prometheus-kafka-adapter

COPY go.mod .
COPY go.sum .
RUN go mod download

ADD . /src/prometheus-kafka-adapter

RUN go build -o /prometheus-kafka-adapter
RUN go test ./...

FROM docker.internal.digitalocean.com/base/debian-grande:latest

COPY schemas/metric.avsc /schemas/metric.avsc
COPY --from=build /prometheus-kafka-adapter /

CMD /prometheus-kafka-adapter

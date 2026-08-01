FROM golang:1.26-bookworm AS builder
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends libpcap-dev \
    && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /out/apiserver ./cmd/apiserver

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends libpcap0.8 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/apiserver /apiserver
EXPOSE 8080
ENTRYPOINT ["/apiserver"]

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd/agent ./cmd/agent
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /agent ./cmd/agent
FROM alpine:3.22
RUN mkdir -p /workspace && chown 65534:65534 /workspace && chmod 0755 /workspace
COPY --from=builder /agent /usr/local/bin/agent
RUN chmod 0755 /usr/local/bin/agent
USER 65534:65534
WORKDIR /workspace
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/agent"]

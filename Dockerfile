# Stage 1: compile node + relay for multiple platforms
FROM golang:1.23 AS builder
WORKDIR /src
COPY . .

# Build node
WORKDIR /src/node
RUN --mount=type=cache,target=/go/pkg/mod \
    GOOS=linux   GOARCH=amd64 go build -o /out/node_linux .
RUN --mount=type=cache,target=/go/pkg/mod \
    GOOS=windows GOARCH=amd64 go build -o /out/node.exe .

# Build relay
WORKDIR /src/relay
RUN --mount=type=cache,target=/go/pkg/mod \
    GOOS=linux   GOARCH=amd64 go build -o /out/relay_linux .
RUN --mount=type=cache,target=/go/pkg/mod \
    GOOS=windows GOARCH=amd64 go build -o /out/relay.exe .

# Stage 2: lightweight image with both binaries
FROM alpine:3.20
COPY --from=builder /out/node_linux /usr/local/bin/node
COPY --from=builder /out/relay_linux /usr/local/bin/relay
ENTRYPOINT ["sh"]
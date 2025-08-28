# Stage 1: build node + relay for all platforms
FROM golang:1.23 AS builder
WORKDIR /src
COPY . .

# node
RUN --mount=type=cache,target=/go/pkg/mod \
    GOOS=linux   GOARCH=amd64 go build -C node -o /out/node_linux_amd64 . && \
    GOOS=linux   GOARCH=arm64 go build -C node -o /out/node_linux_arm64 . && \
    GOOS=windows GOARCH=amd64 go build -C node -o /out/node_windows_amd64.exe . && \
    GOOS=windows GOARCH=arm64 go build -C node -o /out/node_windows_arm64.exe .

# relay
RUN --mount=type=cache,target=/go/pkg/mod \
    GOOS=linux   GOARCH=amd64 go build -C relay -o /out/relay_linux_amd64 . && \
    GOOS=linux   GOARCH=arm64 go build -C relay -o /out/relay_linux_arm64 . && \
    GOOS=windows GOARCH=amd64 go build -C relay -o /out/relay_windows_amd64.exe . && \
    GOOS=windows GOARCH=arm64 go build -C relay -o /out/relay_windows_arm64.exe .

# Stage 2: runtime image (ตัวอย่าง Linux/amd64)
FROM alpine:3.20
COPY --from=builder /out/node_linux_amd64  /usr/local/bin/node
COPY --from=builder /out/relay_linux_amd64 /usr/local/bin/relay
ENTRYPOINT ["sh"]

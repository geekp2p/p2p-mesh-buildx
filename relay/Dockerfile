FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN apk add --no-cache upx
RUN CGO_ENABLED=0 go build -o /bin/p2p-relay . && upx /bin/p2p-relay

FROM alpine:3.20
WORKDIR /app
COPY --from=build /bin/p2p-relay /usr/local/bin/p2p-relay
VOLUME ["/data"]
ENV RELAY_LISTEN=/ip4/0.0.0.0/tcp/4003
ENTRYPOINT ["/usr/local/bin/p2p-relay"]
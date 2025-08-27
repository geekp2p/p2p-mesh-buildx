FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/p2p-node .

FROM alpine:3.20
WORKDIR /app
COPY --from=build /bin/p2p-node /usr/local/bin/p2p-node
COPY --from=build /src/web /app/web
VOLUME ["/data"]
ENV APP_ROOM=my-room
ENTRYPOINT ["/usr/local/bin/p2p-node"]
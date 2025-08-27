# Building Single Binary for Node and Relay

This project contains two Go programs: the peer-to-peer **node** and the **relay**.
Each can be compiled into a standalone binary for different operating systems
without needing Docker.

## Prerequisites
- [Go](https://go.dev/) 1.20 or newer installed on your build machine.
- The commands below use `CGO_ENABLED=0` to disable CGO so that the resulting
  binaries are statically linked and do not depend on external runtime files.

## Build the Node binary
```bash
cd node
# Linux (amd64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o node
# Windows (amd64)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o node.exe
```

## Build the Relay binary
```bash
cd relay
# Linux (amd64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o relay
# Windows (amd64)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o relay.exe
```

Each command produces a single standalone binary (`node`, `node.exe`, `relay`,
`relay.exe`) that can be distributed and executed on the target operating system
without Docker.
# p2p-mesh

A peer-to-peer mesh networking project written in Go and containerized with Docker.
Each node runs independently with a persistent unique PeerID and can discover peers automatically on LAN or over the internet.
The system is serverless by design, with optional relay support for guaranteed connectivity behind restrictive NATs.

## ✨ Features
- ✅ Unique peer IDs for nodes and relays (Ed25519, persisted in volume)
- ✅ Hardware-derived default nicknames (MAC + CPU ID) to distinguish nodes
- ✅ LAN discovery using mDNS
- ✅ NAT traversal with AutoNAT, UPnP, NAT-PMP, and hole punching (DCUtR)
- ✅ GossipSub pub/sub messaging between peers
- ✅ Optional Circuit Relay v2 for guaranteed connectivity
- ✅ Global peer discovery using Kademlia DHT with optional bootstrap peers
- ✅ Docker Compose setup for easy multi-node deployment
## 🖧 LAN Deployment

If all peers are on the same LAN and can reach each other directly, run only node instances—no relay is needed. At least two nodes must be running to communicate. Add a relay only when NAT or firewall rules block direct connections, optionally deploying it inside the LAN to help traverse those restrictions.

## 📦 Quick Start

Clone and run two local nodes in Docker:

```bash
git clone https://github.com/geekp2p/p2p-mesh.git
cd p2p-mesh
cp .env.example .env   # edit APP_ROOM to pick a room name
docker compose --env-file .env up --build
```

All containers read configuration from the `.env` file. Peers that use the
same `APP_ROOM` value will automatically discover and join each other.

## 🔌 Enabling Relay Client

Enable the relay client if your nodes must dial through a public relay server. Set
`ENABLE_RELAY_CLIENT` and provide the relay's multiaddress in the `.env` file
before starting the containers:

```bash
echo "ENABLE_RELAY_CLIENT=true" >> .env
echo "RELAY_ADDR=/ip4/<RELAY_IP>/tcp/4003/p2p/<RELAY_PEER_ID>" >> .env
docker compose --env-file .env up --build
```
Multiple relay addresses can be provided comma-separated in `RELAY_ADDR`. The node keeps trying to stay connected and will automatically switch if a relay becomes unavailable.


## 💬 Chat

Open the chat web UI for each node in your browser:

- http://localhost:3001 for `node1`
- http://localhost:3002 for `node2`

Enter a nickname when prompted and start chatting. Messages will be broadcast to all peers connected to the mesh. If left blank, a nickname based on the node's MAC address and CPU ID is generated automatically.

## 🌍 Bootstrapping & DHT

Nodes can discover each other globally using a Kademlia DHT. Provide one or more
public bootstrap peers via the `BOOTSTRAP_PEERS` variable in `.env` or the
`bootstrap_peers` entry in `config.yaml`:

```bash
echo "BOOTSTRAP_PEERS=/ip4/<NODE_IP>/tcp/4001/p2p/<NODE_PEER_ID>,/dns4/example.com/tcp/4001/p2p/<NODE_PEER_ID>" >> .env
```

Each address must be a full multiaddress **for another node** (not a relay)
including its peer ID. Nodes will connect to the bootstrap peers and announce
themselves on the DHT so that others can find and communicate with them. If you
see `DHT advertise error: failed to find any peer in table`, ensure that at
least one bootstrap peer is reachable and running the DHT.

If no bootstrap peers are specified or the provided ones are unreachable, the
node automatically falls back to any peers recorded in
`/data/known_peers.txt`. Every successful connection is appended to this file,
allowing future runs to reuse previously contacted peers as implicit
bootstrappers.

## 🔄 Auto-Relay Fallback & Peer Persistence

Every node saves the multiaddresses of connected peers to `/data/known_peers.txt`.
The list is reused on startup for bootstrapping and also as a relay fallback.
When `ENABLE_RELAY_CLIENT=true` and none of the configured relays are reachable,
the node will iterate through the stored addresses and attempt to reserve a
relay slot with any reachable public peer. This lets private nodes recover
connectivity through whichever public node comes back online.

```bash
# First run with a known public relay
ENABLE_RELAY_CLIENT=true
RELAY_ADDR=/ip4/1.2.3.4/tcp/4003/p2p/<RELAY_PEER_ID>

# Subsequent runs can fall back to stored peers even if RELAY_ADDR is empty
ENABLE_RELAY_CLIENT=true
RELAY_ADDR=
```

Remove `/data/known_peers.txt` to clear remembered peers.

Nodes with public reachability (detected automatically or via `ANNOUNCE_ADDRS`)
also publish themselves on the DHT as bootstrap providers. All peers
periodically query the DHT for these providers and try to connect, enabling the
mesh to learn about new public nodes without manual configuration.

## 🌐 Announcing Public Addresses

Containers typically advertise their internal addresses (e.g. `127.0.0.1` or
`172.x.x.x`). If you want other machines to dial your relay or nodes directly,
override the announced addresses with the `ANNOUNCE_ADDRS` environment variable.
The Compose file exposes helper variables so you can set them in `.env`:

```bash
RELAY_ANNOUNCE=/ip4/<PUBLIC_IP>/tcp/4003
NODE1_ANNOUNCE=/ip4/<PUBLIC_IP>/tcp/4001
NODE2_ANNOUNCE=/ip4/<PUBLIC_IP>/tcp/4002
```

Multiple addresses can be provided comma-separated. Peers will advertise these
public addresses in addition to their default ones, improving reachability when
running behind NAT or in Docker.

## 🔧 Example Configurations

Below are sample configurations for nodes behind NAT and nodes with a public IP.
Replace placeholders such as `<RELAY_PEER_ID>` and `<NODE_PEER_ID>` with actual
values from your deployment.

### Behind NAT (no public IP)

`.env`

```env
APP_ROOM=my-room
RELAY_ADDR=/ip4/<RELAY_IP>/tcp/4003/p2p/<RELAY_PEER_ID>
ENABLE_RELAY_CLIENT=true
ENABLE_HOLEPUNCH=true
ENABLE_UPNP=true
BOOTSTRAP_PEERS=/ip4/<NODE_IP>/tcp/4001/p2p/<NODE_PEER_ID>
```

`config.yaml`

```yaml
app_room: my-room
relay_addr: /ip4/<RELAY_IP>/tcp/4003/p2p/<RELAY_PEER_ID>
enable_relay_client: true
enable_holepunch: true
enable_upnp: true
bootstrap_peers:
  - /ip4/<NODE_IP>/tcp/4001/p2p/<NODE_PEER_ID>
```

### With a public IP

`.env`

```env
APP_ROOM=my-room
RELAY_ADDR=/ip4/<RELAY_IP>/tcp/4003/p2p/<RELAY_PEER_ID>
ENABLE_RELAY_CLIENT=true
ENABLE_HOLEPUNCH=true
ENABLE_UPNP=true
BOOTSTRAP_PEERS=/ip4/<NODE_IP>/tcp/4001/p2p/<NODE_PEER_ID>
NODE1_ANNOUNCE=/ip4/<YOUR_PUBLIC_IP>/tcp/4001
RELAY_ANNOUNCE=/ip4/<YOUR_PUBLIC_IP>/tcp/4003
```

`config.yaml`

```yaml
app_room: my-room
relay_addr: /ip4/<RELAY_IP>/tcp/4003/p2p/<RELAY_PEER_ID>
enable_relay_client: true
enable_holepunch: true
enable_upnp: true
bootstrap_peers:
  - /ip4/<NODE_IP>/tcp/4001/p2p/<NODE_PEER_ID>
announce_addrs:
  - /ip4/<YOUR_PUBLIC_IP>/tcp/4001
```
### Example: public relay on 103.13.31.47

If your relay runs on host `103.13.31.47` and prints `Relay PeerID: 12D3KooWCLpgD9xrTETP7Yn6NrueDoqTDquvLN9Gqe1WLoHrAYzj`, you can use these settings:

`.env`
```env
APP_ROOM=my-room
RELAY_LISTEN=/ip4/0.0.0.0/tcp/4003
RELAY_ADDR=/ip4/103.13.31.47/tcp/4003/p2p/12D3KooWCLpgD9xrTETP7Yn6NrueDoqTDquvLN9Gqe1WLoHrAYzj
ENABLE_RELAY_CLIENT=true
ENABLE_HOLEPUNCH=true
ENABLE_UPNP=true
BOOTSTRAP_PEERS=/ip4/103.13.31.47/tcp/4001/p2p/<NODE_PEER_ID>
NODE1_ANNOUNCE=/ip4/103.13.31.47/tcp/4001
RELAY_ANNOUNCE=/ip4/103.13.31.47/tcp/4003
```

`<NODE_PEER_ID>` is printed when you start your node; other peers use it as a bootstrap address.

`config.yaml`
```yaml
app_room: my-room
relay_listen: /ip4/0.0.0.0/tcp/4003
relay_addr: /ip4/103.13.31.47/tcp/4003/p2p/12D3KooWCLpgD9xrTETP7Yn6NrueDoqTDquvLN9Gqe1WLoHrAYzj
enable_relay_client: true
enable_holepunch: true
enable_upnp: true
bootstrap_peers:
  - /ip4/103.13.31.47/tcp/4001/p2p/<NODE_PEER_ID>
announce_addrs:
  - /ip4/103.13.31.47/tcp/4003
  - /ip4/103.13.31.47/tcp/4001
```

## 🐳 Build Node and Relay binaries with Docker Buildx

The repository includes a root `Dockerfile` that builds both the node and relay for Linux and Windows in one pass. Use Docker Buildx to produce standalone binaries:

```bash
docker buildx build --output type=local,dest=./bin .
```

The resulting `./bin/out/` directory contains:
- node_linux and node.exe
- relay_linux and relay.exe

The node binary already embeds its web UI via `go:embed`, so no extra files are required.

## 🛠 Manual Build

### Node

```bash
cd node
go build -o p2p-node .
./p2p-node
```

### Relay

```bash
cd relay
go build -o p2p-relay .
./p2p-relay
```

## 🧩 Development Notes

When extending the app, ensure that new protocol IDs (PIDs) and protocol
names follow GitHub's naming conventions (lowercase and hyphen-separated).
Consistent naming helps avoid mismatched protocols that can lead to
bootstrap issues.
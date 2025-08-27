package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	routingdisc "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/host/autonat"
	pstoremem "github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	clientv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	tcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"

	dht "github.com/libp2p/go-libp2p-kad-dht"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ma "github.com/multiformats/go-multiaddr"

	cid "github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"

	"github.com/joho/godotenv"
)

const keyFile = "/data/peerkey.bin"

var bootstrapCID = func() cid.Cid {
	h, _ := mh.Sum([]byte("mesh-bootstrap"), mh.SHA2_256, -1)
	return cid.NewCidV1(cid.Raw, h)
}()

func loadOrCreateKey() (crypto.PrivKey, error) {
	_ = os.MkdirAll(filepath.Dir(keyFile), 0o755)
	if b, err := os.ReadFile(keyFile); err == nil && len(b) == ed25519.PrivateKeySize {
		return crypto.UnmarshalEd25519PrivateKey(b)
	}
	_, pk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyFile, []byte(pk), 0o600); err != nil {
		return nil, err
	}
	return crypto.UnmarshalEd25519PrivateKey([]byte(pk))
}

type mdnsNotifee struct{ h host.Host }

// HandlePeerFound attempts to connect to peers discovered via mDNS.
func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	fmt.Printf("[mDNS] found %s\n", short(pi.ID))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = n.h.Connect(ctx, pi)
}

type ipValidator struct{}

func (ipValidator) Validate(string, []byte) error        { return nil }
func (ipValidator) Select(string, [][]byte) (int, error) { return 0, nil }

func getenvBool(k string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "y"
}

func main() {
	_ = godotenv.Load(".env")
	cfg := loadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// env/config
	room := firstNonEmpty(os.Getenv("APP_ROOM"), cfg.AppRoom, "my-room")
	listenTCP := firstNonEmpty(os.Getenv("LISTEN_TCP"), "/ip4/0.0.0.0/tcp/4001")
	listenQUIC := os.Getenv("LISTEN_QUIC") // e.g. "/ip4/0.0.0.0/udp/4001/quic-v1"
	relayAddrStr := firstNonEmpty(os.Getenv("RELAY_ADDR"), cfg.RelayAddr)
	relayAddrs := []ma.Multiaddr{}
	if relayAddrStr != "" {
		for _, addr := range strings.Split(relayAddrStr, ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			maddr, err := ma.NewMultiaddr(addr)
			if err != nil {
				fmt.Println("Invalid RELAY_ADDR, skipping:", err)
				continue
			}
			relayAddrs = append(relayAddrs, maddr)
		}
	}
	relayCh := make(chan ma.Multiaddr, 16)
	enableRelayClient := getenvBool("ENABLE_RELAY_CLIENT", cfg.EnableRelayClient)
	enableHP := getenvBool("ENABLE_HOLEPUNCH", cfg.EnableHolePunch)
	enableUPnP := getenvBool("ENABLE_UPNP", cfg.EnableUPnP)
	peerDB := newPeerStore("/data/known_peers.txt")
	publicIPs := detectPublicIPs()
	bootstrapPeers := cfg.BootstrapPeers
	envProvided := false
	if envPeers := os.Getenv("BOOTSTRAP_PEERS"); envPeers != "" {
		bootstrapPeers = strings.Split(envPeers, ",")
		envProvided = true
	} else if len(bootstrapPeers) > 0 {
		envProvided = true
	}
	if len(bootstrapPeers) == 0 {
		bootstrapPeers = peerDB.List()
	}
	announceAddrs := []ma.Multiaddr{}
	seeds := cfg.AnnounceAddrs
	if env := os.Getenv("ANNOUNCE_ADDRS"); env != "" {
		seeds = strings.Split(env, ",")
	}
	for _, s := range seeds {
		m, err := ma.NewMultiaddr(strings.TrimSpace(s))
		if err != nil {
			if s != "" {
				fmt.Println("Invalid announce addr, skipping:", err)
			}
			continue
		}
		announceAddrs = append(announceAddrs, m)
	}

	// automatically announce detected public IPs using the listen ports
	var tcpPort, udpPort string
	if listenTCP != "" {
		if m, err := ma.NewMultiaddr(listenTCP); err == nil {
			tcpPort, _ = m.ValueForProtocol(ma.P_TCP)
		}
	}
	if listenQUIC != "" {
		if m, err := ma.NewMultiaddr(listenQUIC); err == nil {
			udpPort, _ = m.ValueForProtocol(ma.P_UDP)
		}
	}
	for _, ip := range publicIPs {
		ipType := "ip4"
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
			ipType = "ip6"
		}
		if tcpPort != "" {
			if m, err := ma.NewMultiaddr(fmt.Sprintf("/%s/%s/tcp/%s", ipType, ip, tcpPort)); err == nil {
				announceAddrs = append(announceAddrs, m)
			}
		}
		if udpPort != "" {
			if m, err := ma.NewMultiaddr(fmt.Sprintf("/%s/%s/udp/%s/quic-v1", ipType, ip, udpPort)); err == nil {
				announceAddrs = append(announceAddrs, m)
			}
		}
	}

	// key & in-memory peerstore
	priv, err := loadOrCreateKey()
	must(err)
	ps, err := pstoremem.NewPeerstore()
	must(err)
	defer ps.Close()

	// resource manager (safe defaults)
	rmgr, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.AutoScale()))
	must(err)

	// host options
	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.Peerstore(ps),
		libp2p.ResourceManager(rmgr),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.Transport(tcp.NewTCPTransport),
	}
	if listenTCP != "" {
		opts = append(opts, libp2p.ListenAddrStrings(listenTCP))
	}
	if listenQUIC != "" {
		opts = append(opts, libp2p.Transport(quic.NewTransport), libp2p.ListenAddrStrings(listenQUIC))
	}
	if enableUPnP {
		opts = append(opts, libp2p.NATPortMap())
	}
	if enableRelayClient {
		opts = append(opts, libp2p.EnableRelay()) // client relay
	}
	if enableHP {
		opts = append(opts, libp2p.EnableHolePunching())
	}
	if len(announceAddrs) > 0 {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			return append(addrs, announceAddrs...)
		}))
	}

	h, err := libp2p.New(opts...)
	must(err)
	defer h.Close()

	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(net network.Network, conn network.Conn) {
			peerDB.Add(conn.RemoteMultiaddr(), conn.RemotePeer())
		},
		DisconnectedF: func(net network.Network, conn network.Conn) {
			go reconnectOnDisconnect(ctx, h, conn.RemotePeer())
		},
	})

	fmt.Printf("PeerID: %s\n", h.ID().String())
	for _, a := range h.Addrs() {
		fmt.Printf("Listen: %s/p2p/%s\n", a, h.ID())
	}

	// AutoNAT (help NAT type detection)
	_, _ = autonat.New(h)

	// mDNS for LAN
	ser := mdns.NewMdnsService(h, room, &mdnsNotifee{h: h})
	defer ser.Close()

	// Maintain connections to any configured relay addresses.
	if len(relayAddrs) > 0 {
		go maintainRelayConnections(ctx, h, relayAddrs, peerDB, relayCh)
	}

	// connect to any configured bootstrap peers
	bootstrapped := false
	for _, addr := range bootstrapPeers {
		a := strings.TrimSpace(addr)
		if a == "" {
			continue
		}
		maddr, err := ma.NewMultiaddr(a)
		if err != nil {
			fmt.Println("Invalid bootstrap addr, skipping:", err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			fmt.Println("bootstrap AddrInfo error:", err)
			continue
		}
		if pi.ID == h.ID() {
			continue // skip connecting to ourselves
		}
		h.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.PermanentAddrTTL)
		if err := h.Connect(ctx, *pi); err != nil {
			fmt.Println("bootstrap connect failed:", err)
		} else {
			bootstrapped = true
			fmt.Printf("Bootstrapped to %s\n", short(pi.ID))
		}
	}
	if !bootstrapped && envProvided {
		for _, addr := range peerDB.List() {
			a := strings.TrimSpace(addr)
			if a == "" {
				continue
			}
			maddr, err := ma.NewMultiaddr(a)
			if err != nil {
				fmt.Println("Invalid fallback addr, skipping:", err)
				continue
			}
			pi, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				fmt.Println("fallback AddrInfo error:", err)
				continue
			}
			if pi.ID == h.ID() {
				continue // skip ourselves from peer DB
			}
			h.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.PermanentAddrTTL)
			if err := h.Connect(ctx, *pi); err != nil {
				fmt.Println("fallback connect failed:", err)
			} else {
				bootstrapped = true
				fmt.Printf("Bootstrapped to %s\n", short(pi.ID))
			}
		}
	}

	// DHT for global peer discovery
	kdht, err := dht.New(ctx, h,
		dht.ProtocolPrefix("/mesh"),
		dht.NamespacedValidator("publicip", ipValidator{}),
	)
	must(err)
	must(kdht.Bootstrap(ctx))
	rdisc := routingdisc.NewRoutingDiscovery(kdht)
	if len(publicIPs) > 0 {
		key := "/publicip/" + h.ID().String()
		if err := kdht.PutValue(ctx, key, []byte(strings.Join(publicIPs, ","))); err != nil {
			fmt.Println("DHT put public IPs:", err)
		}
	}
	if len(publicIPs) > 0 || len(announceAddrs) > 0 {
		go func() {
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				if err := kdht.Provide(ctx, bootstrapCID, true); err != nil {
					fmt.Println("DHT provide bootstrap:", err)
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			if kdht.RoutingTable().Size() > 0 {
				provCh := kdht.FindProvidersAsync(ctx, bootstrapCID, 20)
				for p := range provCh {
					if p.ID == h.ID() {
						continue
					}
					fmt.Printf("[DHT bootstrap] found %s\n", short(p.ID))
					_ = h.Connect(ctx, p)
				}
				if _, err := rdisc.Advertise(ctx, "room:"+room); err != nil {
					fmt.Println("DHT advertise error:", err)
				}
				peerCh, err := rdisc.FindPeers(ctx, "room:"+room)
				if err != nil {
					fmt.Println("DHT find peers:", err)
				} else {
					for p := range peerCh {
						if p.ID == h.ID() {
							continue
						}
						fmt.Printf("[DHT] found %s\n", short(p.ID))
						_ = h.Connect(ctx, p)
					}
				}
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()

	// PubSub topic
	psub, err := pubsub.NewGossipSub(ctx, h)
	must(err)
	topic, err := psub.Join("room:" + room)
	must(err)
	sub, err := topic.Subscribe()
	must(err)

	relayTopic, err := psub.Join("relays")
	must(err)
	relaySub, err := relayTopic.Subscribe()
	must(err)
	go relayAnnounce(ctx, h, relayTopic, relaySub, relayCh)

	RunWebGateway(ctx, h, psub, topic, sub, room)

	// simple handler: print any direct stream
	h.SetStreamHandler("/echo/1.0.0", func(s network.Stream) {
		defer s.Close()
		io.Copy(os.Stdout, s)
	})

	// publisher: read stdin and publish to the topic
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if err := topic.Publish(ctx, []byte(line)); err != nil {
				fmt.Println("publish error:", err)
			}
		}
	}()

	go watchdogPeerConnections(ctx, h, peerDB, bootstrapPeers)

	// wait signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

func maintainRelayConnections(ctx context.Context, h host.Host, addrs []ma.Multiaddr, ps *peerStore, announce chan<- ma.Multiaddr) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if !isAnyRelayConnected(h, addrs) {
			connected := false
			for _, maddr := range addrs {
				if err := connectToRelay(ctx, h, maddr); err == nil {
					fmt.Println("Relay connected via", maddr.String())
					if announce != nil {
						select {
						case announce <- maddr:
						default:
						}
					}
					connected = true
					break
				}
			}
			if !connected {
				for _, addr := range ps.List() {
					maddr, err := ma.NewMultiaddr(addr)
					if err != nil {
						continue
					}
					if err := connectToRelay(ctx, h, maddr); err == nil {
						fmt.Println("Relay fallback via", maddr.String())
						addrs = append(addrs, maddr)
						if announce != nil {
							select {
							case announce <- maddr:
							default:
							}
						}
						break
					}
				}
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func relayAnnounce(ctx context.Context, h host.Host, topic *pubsub.Topic, sub *pubsub.Subscription, in <-chan ma.Multiaddr) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case maddr := <-in:
				_ = topic.Publish(ctx, []byte(maddr.String()))
			}
		}
	}()
	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		addrStr := strings.TrimSpace(string(msg.Data))
		maddr, err := ma.NewMultiaddr(addrStr)
		if err != nil {
			continue
		}
		if err := connectToRelay(ctx, h, maddr); err == nil {
			fmt.Println("Relay connected via announcement", maddr.String())
		}
	}
}

func isAnyRelayConnected(h host.Host, addrs []ma.Multiaddr) bool {
	for _, maddr := range addrs {
		pi, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			continue
		}
		if h.Network().Connectedness(pi.ID) == network.Connected {
			return true
		}

	}
	return false
}

func connectToRelay(ctx context.Context, h host.Host, maddr ma.Multiaddr) error {
	pi, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return err
	}
	h.Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.PermanentAddrTTL)
	if err := h.Connect(ctx, *pi); err != nil {
		return err
	}
	// Reserve slot (optional; ensures we can use relay/circuit)
	_, err = clientv2.Reserve(ctx, h, *pi)
	return err
}

func reconnectOnDisconnect(ctx context.Context, h host.Host, id peer.ID) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		pi := peer.AddrInfo{ID: id, Addrs: h.Peerstore().Addrs(id)}
		if len(pi.Addrs) == 0 {
			continue
		}
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := h.Connect(dialCtx, pi); err == nil {
			cancel()
			fmt.Printf("Reconnected to %s\n", short(id))
			return
		}
		cancel()
		if backoff < time.Minute {
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
		}
	}
}

func watchdogPeerConnections(ctx context.Context, h host.Host, ps *peerStore, bootstrap []string) {
	base := 30 * time.Second
	delay := base
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if len(h.Network().Peers()) > 0 {
			delay = base
			timer.Reset(delay)
			continue
		}
		attempt := func(addr string) bool {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				return false
			}
			maddr, err := ma.NewMultiaddr(addr)
			if err != nil {
				return false
			}
			pi, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil || pi.ID == h.ID() {
				return false
			}
			dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = h.Connect(dialCtx, *pi)
			cancel()
			if err == nil {
				fmt.Printf("[watchdog] connected to %s\n", short(pi.ID))
				return true
			}
			return false
		}
		for _, addr := range bootstrap {
			if attempt(addr) {
				break
			}
		}
		if len(h.Network().Peers()) == 0 {
			for _, addr := range ps.List() {
				if attempt(addr) {
					break
				}
			}
		}
		if len(h.Network().Peers()) == 0 {
			if delay < 5*time.Minute {
				delay *= 2
				if delay > 5*time.Minute {
					delay = 5 * time.Minute
				}
			}
		} else {
			delay = base
		}
		timer.Reset(delay)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
func short(id peer.ID) string {
	b := []byte(id)
	if len(b) > 6 {
		return hex.EncodeToString(b[:6])
	}
	return id.String()
}

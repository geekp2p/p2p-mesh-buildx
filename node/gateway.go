package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	cpuid "github.com/klauspost/cpuid/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	host "github.com/libp2p/go-libp2p/core/host"
)

type ChatMsg struct {
	From string `json:"from"`
	ID   string `json:"id"`
	Text string `json:"text"`
	Ts   int64  `json:"ts"`
}

type WSClient struct {
	conn *websocket.Conn
	send chan []byte
}

type Gateway struct {
	h        host.Host
	psub     *pubsub.PubSub
	topic    *pubsub.Topic
	sub      *pubsub.Subscription
	clients  map[*WSClient]bool
	mu       sync.RWMutex
	upgrader websocket.Upgrader
	nick     string
	room     string
}

func NewGateway(h host.Host, psub *pubsub.PubSub, topic *pubsub.Topic, sub *pubsub.Subscription, nick, room string) *Gateway {
	return &Gateway{
		h:       h,
		psub:    psub,
		topic:   topic,
		sub:     sub,
		clients: make(map[*WSClient]bool),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		nick: nick,
		room: room,
	}
}

func (g *Gateway) Start(ctx context.Context, webAddr string) error {

	// consume pubsub -> fanout to websockets
	go g.consume(ctx)

	http.HandleFunc("/", g.serveIndex)
	http.HandleFunc("/ws", g.serveWS)
	http.HandleFunc("/config", g.handleConfig)

	log.Printf("🌐 Chat UI on http://0.0.0.0%s  (room=%s nick=%s)\n", webAddr, g.room, g.nick)
	return http.ListenAndServe(webAddr, nil)
}

func (g *Gateway) consume(ctx context.Context) {
	for {
		g.mu.RLock()
		sub := g.sub
		g.mu.RUnlock()
		msg, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Println("pubsub sub.Next:", err)
			continue
		}
		var cm ChatMsg
		if err := json.Unmarshal(msg.Data, &cm); err != nil {
			continue
		}
		g.broadcast(cm)
	}
}

//go:embed web/index.html
var indexHTML []byte

func (g *Gateway) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (g *Gateway) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &WSClient{conn: conn, send: make(chan []byte, 32)}
	g.mu.Lock()
	g.clients[client] = true
	g.mu.Unlock()

	// writer
	go func() {
		for b := range client.send {
			_ = client.conn.WriteMessage(websocket.TextMessage, b)
		}
	}()

	// reader -> publish to GossipSub
	go func() {
		defer func() {
			g.mu.Lock()
			delete(g.clients, client)
			g.mu.Unlock()
			_ = conn.Close()
		}()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			g.mu.RLock()
			nick := g.nick
			g.mu.RUnlock()
			cm := ChatMsg{
				From: nick,
				ID:   g.h.ID().String(),
				Text: string(data),
				Ts:   time.Now().Unix(),
			}
			payload, _ := json.Marshal(cm)
			if err := g.topic.Publish(context.Background(), payload); err != nil {
				log.Println("topic.Publish:", err)
			}
		}
	}()
}

func (g *Gateway) broadcast(cm ChatMsg) {
	b, _ := json.Marshal(cm)
	g.mu.RLock()
	defer g.mu.RUnlock()
	for c := range g.clients {
		select {
		case c.send <- b:
		default:
		}
	}
}

func (g *Gateway) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		g.mu.RLock()
		resp := struct {
			Nick string `json:"nick"`
			Room string `json:"room"`
			ID   string `json:"id"`
		}{g.nick, g.room, g.h.ID().String()}
		g.mu.RUnlock()
		_ = json.NewEncoder(w).Encode(resp)
	case http.MethodPost:
		var req struct {
			Nick string `json:"nick"`
			Room string `json:"room"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Nick != "" {
			g.mu.Lock()
			g.nick = req.Nick
			g.mu.Unlock()
		}
		if req.Room != "" && req.Room != g.room {
			if err := g.setRoom(req.Room); err != nil {
				http.Error(w, "room change failed", http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (g *Gateway) setRoom(r string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sub != nil {
		g.sub.Cancel()
	}
	if g.topic != nil {
		g.topic.Close()
	}
	topic, err := g.psub.Join("room:" + r)
	if err != nil {
		return err
	}
	sub, err := topic.Subscribe()
	if err != nil {
		topic.Close()
		return err
	}
	g.topic = topic
	g.sub = sub
	g.room = r
	return nil
}

func defaultNick() string {
	var mac string
	if ifs, err := net.Interfaces(); err == nil {
		for _, iface := range ifs {
			if iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) > 0 {
				mac = iface.HardwareAddr.String()
				break
			}
		}
	}
	cpu := cpuid.CPU.BrandName
	sum := sha256.Sum256([]byte(mac + cpu))
	return hex.EncodeToString(sum[:6])
}

func RunWebGateway(ctx context.Context, h host.Host, psub *pubsub.PubSub, topic *pubsub.Topic, sub *pubsub.Subscription, room string) {
	webAddr := os.Getenv("WEB_ADDR")
	if webAddr == "" {
		webAddr = ":3000"
	}
	nick := os.Getenv("NODE_NICK")
	if nick == "" {
		nick = defaultNick()
	}
	gw := NewGateway(h, psub, topic, sub, nick, room)
	go func() {
		if err := gw.Start(ctx, webAddr); err != nil {
			log.Println("gateway.Start:", err)
		}
	}()
}

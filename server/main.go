package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	lifetime    = 10 * time.Second
	maxSegments = 16000
	maxBatch    = 128
)

// Coordinates are fractions of the viewport; At is assigned by the server.
type segment struct {
	ID string  `json:"id"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
	At int64   `json:"at"`
}

type message struct {
	Type     string    `json:"type"`
	Now      int64     `json:"now,omitempty"`
	Lifetime int64     `json:"lifetime,omitempty"`
	Segments []segment `json:"segments"`
}

type peer struct {
	ws   *websocket
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func (p *peer) close() {
	p.once.Do(func() {
		close(p.done)
		p.ws.conn.Close()
	})
}

func (p *peer) enqueue(data []byte) {
	select {
	case <-p.done:
	case p.out <- data:
	default:
		// A slow client reconnects for a fresh snapshot instead of growing a queue.
		p.close()
	}
}

func (p *peer) writeLoop() {
	defer p.close()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		var err error
		select {
		case <-p.done:
			return
		case data := <-p.out:
			err = p.ws.writeFrame(opText, data)
		case <-ticker.C:
			err = p.ws.writeFrame(opPing, nil)
		}
		if err != nil {
			return
		}
	}
}

type room struct {
	mu       sync.Mutex
	segments []segment
	peers    map[*peer]struct{}
}

func newRoom() *room {
	return &room{segments: make([]segment, 0), peers: make(map[*peer]struct{})}
}

// Called with mu held. Segments are ordered by the server's receipt time.
func (r *room) prune(now int64) {
	first := 0
	for first < len(r.segments) && now-r.segments[first].At >= lifetime.Milliseconds() {
		first++
	}
	if first > 0 {
		n := copy(r.segments, r.segments[first:])
		clear(r.segments[n:])
		r.segments = r.segments[:n]
	}
}

func (r *room) join(p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	r.prune(now)
	data, _ := json.Marshal(message{"snapshot", now, lifetime.Milliseconds(), r.segments})
	p.enqueue(data)
	r.peers[p] = struct{}{}
}

func (r *room) draw(data []byte) bool {
	var msg message
	if json.Unmarshal(data, &msg) != nil || msg.Type != "draw" || len(msg.Segments) == 0 || len(msg.Segments) > maxBatch {
		return false
	}
	for _, s := range msg.Segments {
		if len(s.ID) == 0 || len(s.ID) > 80 {
			return false
		}
		for _, v := range []float64{s.X1, s.Y1, s.X2, s.Y2} {
			if v < 0 || v > 1 {
				return false
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	r.prune(now)
	for i := range msg.Segments {
		msg.Segments[i].At = now
	}
	r.segments = append(r.segments, msg.Segments...)
	if extra := len(r.segments) - maxSegments; extra > 0 {
		n := copy(r.segments, r.segments[extra:])
		clear(r.segments[n:])
		r.segments = r.segments[:n]
	}
	encoded, _ := json.Marshal(message{Type: "draw", Segments: msg.Segments})
	for p := range r.peers {
		p.enqueue(encoded)
	}
	return true
}

func (r *room) serveWS(w http.ResponseWriter, req *http.Request) {
	ws, err := upgrade(w, req)
	if err != nil {
		return
	}
	p := &peer{ws: ws, out: make(chan []byte, 64), done: make(chan struct{})}
	r.join(p)
	defer func() {
		p.close()
		r.mu.Lock()
		delete(r.peers, p)
		r.mu.Unlock()
	}()
	go p.writeLoop()
	for {
		data, err := ws.readMessage()
		if err != nil {
			return
		}
		if !r.draw(data) {
			ws.writeFrame(opClose, []byte{0x03, 0xf0}) // 1008: invalid application message
			return
		}
	}
}

func (r *room) maintain(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			for p := range r.peers {
				p.close()
			}
			r.mu.Unlock()
			return
		case <-ticker.C:
			r.mu.Lock()
			r.prune(time.Now().UnixMilli())
			r.mu.Unlock()
		}
	}
}

func main() {
	host := flag.String("host", "127.0.0.1", "HTTP/WebSocket bind host (overrides the host in -listen)")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP/WebSocket listen address")
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "host" {
			if *host == "" {
				log.Fatal("-host must not be empty")
			}
			_, port, err := net.SplitHostPort(*listen)
			if err != nil {
				log.Fatalf("invalid -listen address: %v", err)
			}
			*listen = net.JoinHostPort(*host, port)
		}
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := newRoom()
	go r.maintain(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", r.serveWS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	server := &http.Server{
		Addr: *listen, Handler: mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8192,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("bukupan listening on %s", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

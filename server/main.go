package main

import (
	"context"
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
			err = p.ws.writeFrame(opBinary, data)
		case <-ticker.C:
			err = p.ws.writeFrame(opPing, nil)
		}
		if err != nil {
			return
		}
	}
}

type room struct {
	mu          sync.Mutex
	segments    [maxSegments]segment
	head, count int
	lastAt      int64
	peers       map[*peer]struct{}
}

func newRoom() *room {
	return &room{peers: make(map[*peer]struct{})}
}

// Called with mu held. Segments are ordered by the server's receipt time.
func (r *room) prune(now int64) {
	for r.count > 0 && now-r.segments[r.head].At >= lifetime.Milliseconds() {
		r.segments[r.head] = segment{}
		r.head = (r.head + 1) % maxSegments
		r.count--
	}
}

// Called with mu held; no stored segment is moved.
func (r *room) append(s segment) {
	r.segments[(r.head+r.count)%maxSegments] = s
	if r.count == maxSegments {
		r.head = (r.head + 1) % maxSegments
	} else {
		r.count++
	}
}

func (r *room) join(p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	r.prune(now)
	data := encodeHeader(snapshot, r.count, now, uint32(lifetime.Milliseconds()))
	for i := 0; i < r.count; i++ {
		encodeSegment(data[headerSize+i*snapshotRecordSize:], r.segments[(r.head+i)%maxSegments], true)
	}
	p.enqueue(data)
	r.peers[p] = struct{}{}
}

func (r *room) draw(data []byte) bool {
	segments, ok := decodeDraw(data)
	if !ok {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	if now < r.lastAt {
		now = r.lastAt
	}
	r.lastAt = now
	r.prune(now)
	encoded := encodeHeader(serverDraw, len(segments), now, 0)
	for i := range segments {
		segments[i].At = now
		r.append(segments[i])
		encodeSegment(encoded[headerSize+i*recordSize:], segments[i], false)
	}
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

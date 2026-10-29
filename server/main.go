package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type peer struct {
	ws     *websocket
	out    chan []byte
	done   chan struct{}
	once   sync.Once
	queued atomic.Int64
	author string
	area   bounds
	view   int64
}

func (p *peer) close() {
	p.once.Do(func() {
		close(p.done)
		if p.ws != nil {
			p.ws.conn.Close()
		}
	})
}
func (p *peer) enqueue(message response) {
	message.V = protocolVersion
	data, err := json.Marshal(message)
	if err != nil || len(data) > maxOutput {
		p.close()
		return
	}
	if p.queued.Add(int64(len(data))) > maxOutput {
		p.close()
		return
	}
	select {
	case <-p.done:
	case p.out <- data:
	default:
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
			p.queued.Add(-int64(len(data)))
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
	mu    sync.Mutex
	store *boardStore
	peers map[*peer]struct{}
}

func newRoom(store *boardStore) *room { return &room{store: store, peers: make(map[*peer]struct{})} }
func (r *room) join(p *peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[p] = struct{}{}
	p.enqueue(response{Type: "hello", Board: r.store.ID})
}
func (p *peer) failure(err error, id string, view int64) {
	code := "storage"
	switch {
	case errors.Is(err, errInvalid):
		code = "invalid"
	case errors.Is(err, errDense):
		code = "dense"
	default:
		log.Printf("board: %v", err)
	}
	p.enqueue(response{Type: "error", ID: id, View: view, Code: code})
}
func (r *room) handle(p *peer, data []byte) bool {
	req, err := decodeRequest(data)
	if err != nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if req.Board != r.store.ID {
		p.enqueue(response{Type: "error", Code: "board"})
		return true
	}
	if req.Type == "view" {
		if p.author != "" && p.author != req.Author {
			return false
		}
		p.author = req.Author
		p.area = *req.Area
		p.view = req.View
		drawings, revision, e := r.store.View(p.area)
		if e != nil {
			p.failure(e, "", p.view)
			p.view = 0
			return true
		}
		history, e := r.store.History(p.author)
		if e != nil {
			p.failure(e, "", p.view)
			p.view = 0
			return true
		}
		p.enqueue(response{Type: "snapshot", View: p.view, Revision: revision, Drawings: drawings, History: history})
		return true
	}
	op := *req.Edit
	if p.author == "" || p.author != op.Author {
		return false
	}
	changed, e := r.store.Apply(op)
	if e != nil {
		p.failure(e, op.ID, 0)
		return true
	}
	// The transaction is durable before any peer sees the result or receives an ACK.
	history, historyErr := r.store.History(p.author)
	if !changed.Duplicate {
		for other := range r.peers {
			if other.view == 0 {
				continue
			}
			kind := "patch"
			var drawings []drawing
			if len(changed.IDs) > maxViewStrokes {
				kind = "snapshot"
				drawings, _, e = r.store.View(other.area)
			} else {
				drawings, e = r.store.Patch(changed.IDs, other.area)
			}
			if e != nil {
				other.failure(e, "", other.view)
				other.view = 0
				continue
			}
			var ownHistory []string
			if other.author == p.author {
				ownHistory = history
			} else if kind == "snapshot" {
				ownHistory, e = r.store.History(other.author)
				if e != nil {
					other.failure(e, "", other.view)
					continue
				}
			}
			other.enqueue(response{Type: kind, View: other.view, Revision: changed.Revision, Drawings: drawings, History: ownHistory})
		}
	}
	if historyErr != nil {
		p.failure(historyErr, op.ID, 0)
		return true
	}
	p.enqueue(response{Type: "ack", ID: op.ID, Revision: changed.Revision, History: history})
	return true
}
func (r *room) serveWS(w http.ResponseWriter, req *http.Request) {
	ws, err := upgrade(w, req)
	if err != nil {
		return
	}
	p := &peer{ws: ws, out: make(chan []byte, 8), done: make(chan struct{})}
	r.join(p)
	defer func() { p.close(); r.mu.Lock(); delete(r.peers, p); r.mu.Unlock() }()
	go p.writeLoop()
	for {
		data, err := ws.readMessage()
		if err != nil {
			return
		}
		if !r.handle(p, data) {
			ws.writeFrame(opClose, []byte{0x03, 0xf0})
			return
		}
	}
}
func defaultDatabase() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "bukupan", "board.sqlite")
}
func main() {
	host := flag.String("host", "127.0.0.1", "HTTP/WebSocket bind host (overrides the host in -listen)")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP/WebSocket listen address")
	filename := flag.String("db", defaultDatabase(), "persistent SQLite document path")
	backup := flag.String("backup", "", "write a consistent backup to a new file and exit")
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "host" {
			if *host == "" {
				log.Fatal("-host must not be empty")
			}
			_, port, err := net.SplitHostPort(*listen)
			if err != nil {
				log.Fatal(err)
			}
			*listen = net.JoinHostPort(*host, port)
		}
	})
	if *filename == "" {
		log.Fatal("-db must not be empty")
	}
	if *backup != "" {
		if _, err := os.Stat(*filename); err != nil {
			log.Fatal(err)
		}
		store, err := openBoard(*filename)
		if err != nil {
			log.Fatal(err)
		}
		defer store.Close()
		if err = store.Backup(*backup); err != nil {
			log.Fatal(err)
		}
		log.Printf("backup saved: %s", *backup)
		return
	}
	// Only one serving process owns a document; another process may take a backup.
	if err := os.MkdirAll(filepath.Dir(*filename), 0700); err != nil {
		log.Fatal(err)
	}
	lock, err := os.OpenFile(*filename+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		log.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Fatalf("document is already served: %v", err)
	}
	store, err := openBoard(*filename)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	r := newRoom(store)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", r.serveWS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		r.mu.Lock()
		for p := range r.peers {
			p.close()
		}
		r.mu.Unlock()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("bukupan listening on %s; document %s", *listen, *filename)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	stop()
	<-stopped
}

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

var (
	errInvalid = errors.New("invalid operation")
	errDense   = errors.New("view is too dense; zoom in")
	idPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type edit struct {
	ID     string  `json:"id"`
	Author string  `json:"author"`
	Action string  `json:"action,omitempty"`
	Kind   string  `json:"kind"`
	Width  float64 `json:"width,omitempty"`
	Points []point `json:"points,omitempty"`
	Target string  `json:"target,omitempty"`
}
type drawing struct {
	ID    string  `json:"id"`
	Width float64 `json:"width"`
	Lines []line  `json:"lines"`
}
type storedStroke struct {
	id     string
	width  float64
	points []point
	area   bounds
}
type change struct {
	Revision  int64
	IDs       []string
	Duplicate bool
}
type boardStore struct {
	mu sync.Mutex
	db *sql.DB
	ID string
}

func randomID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id[:])
}

func openBoard(filename string) (*boardStore, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	file.Close()
	u := url.URL{Scheme: "file", Path: absolute}
	query := u.Query()
	query.Set("_busy_timeout", "5000")
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "FULL")
	query.Set("_foreign_keys", "on")
	query.Set("_txlock", "immediate")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &boardStore{db: db}
	if err = s.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *boardStore) initialize() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("database schema %d is newer than this server", version)
	}
	if version == 0 {
		_, err = tx.Exec(`
CREATE TABLE board (id TEXT NOT NULL, revision INTEGER NOT NULL);
CREATE TABLE actions (id TEXT PRIMARY KEY, author TEXT NOT NULL, kind TEXT NOT NULL, revision INTEGER NOT NULL, undone INTEGER NOT NULL DEFAULT 0);
CREATE INDEX actions_author ON actions(author,undone,revision DESC);
CREATE TABLE strokes (id TEXT PRIMARY KEY, action TEXT NOT NULL REFERENCES actions(id), width REAL NOT NULL, points BLOB NOT NULL, x0 REAL NOT NULL,y0 REAL NOT NULL,x1 REAL NOT NULL,y1 REAL NOT NULL, visible INTEGER NOT NULL DEFAULT 1);
CREATE INDEX strokes_action ON strokes(action);
CREATE TABLE cells (x INTEGER NOT NULL,y INTEGER NOT NULL,stroke TEXT NOT NULL REFERENCES strokes(id),PRIMARY KEY(x,y,stroke)) WITHOUT ROWID;
CREATE TABLE cuts (stroke TEXT NOT NULL REFERENCES strokes(id),eraser TEXT NOT NULL REFERENCES actions(id),segment INTEGER NOT NULL,lo REAL NOT NULL,hi REAL NOT NULL);
CREATE INDEX cuts_stroke ON cuts(stroke);
CREATE INDEX cuts_eraser ON cuts(eraser);
CREATE UNIQUE INDEX cuts_unique ON cuts(stroke,eraser,segment,lo,hi);
CREATE TABLE receipts (id TEXT PRIMARY KEY,digest BLOB NOT NULL,revision INTEGER NOT NULL);
PRAGMA user_version=1;`)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO board VALUES (?,0)", randomID()); err != nil {
			return err
		}
	}
	if err = tx.QueryRow("SELECT id FROM board").Scan(&s.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *boardStore) Close() error { return s.db.Close() }

func (op edit) validate() error {
	if !idPattern.MatchString(op.ID) || !idPattern.MatchString(op.Author) {
		return errInvalid
	}
	if op.Kind == "undo" {
		if !idPattern.MatchString(op.Target) || len(op.Points) != 0 || op.Width != 0 || op.Action != "" {
			return errInvalid
		}
		return nil
	}
	if (op.Kind != "pen" && op.Kind != "erase") || !idPattern.MatchString(op.Action) || op.Target != "" || !finite(op.Width) || op.Width < 0.25 || op.Width > 256 || len(op.Points) < 1 || len(op.Points) > 128 {
		return errInvalid
	}
	for _, p := range op.Points {
		for _, v := range p {
			if !finite(v) || math.Abs(v) > worldLimit-512 {
				return errInvalid
			}
		}
	}
	if !pathBounds(op.Points, op.Width/2).valid(maxPathSpan) {
		return errInvalid
	}
	return nil
}

func (s *boardStore) Apply(op edit) (change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := op.validate(); err != nil {
		return change{}, err
	}
	body, _ := json.Marshal(op)
	digest := sha256.Sum256(body)
	tx, err := s.db.Begin()
	if err != nil {
		return change{}, err
	}
	defer tx.Rollback()
	var prior []byte
	var revision int64
	err = tx.QueryRow("SELECT digest,revision FROM receipts WHERE id=?", op.ID).Scan(&prior, &revision)
	if err == nil {
		if !bytes.Equal(prior, digest[:]) {
			return change{}, errInvalid
		}
		return change{Revision: revision, Duplicate: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return change{}, err
	}
	if err = tx.QueryRow("UPDATE board SET revision=revision+1 RETURNING revision").Scan(&revision); err != nil {
		return change{}, err
	}
	var ids []string
	if op.Kind == "undo" {
		var kind string
		if err = tx.QueryRow("SELECT kind FROM actions WHERE id=? AND author=? AND undone=0", op.Target, op.Author).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
			return change{}, errInvalid
		} else if err != nil {
			return change{}, err
		}
		query := "SELECT id FROM strokes WHERE action=?"
		if kind == "erase" {
			query = "SELECT DISTINCT stroke FROM cuts WHERE eraser=?"
		}
		rows, e := tx.Query(query, op.Target)
		if e != nil {
			return change{}, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return change{}, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return change{}, e
		}
		if _, err = tx.Exec("UPDATE actions SET undone=1 WHERE id=?", op.Target); err != nil {
			return change{}, err
		}
	} else {
		var author, kind string
		var undone int
		err = tx.QueryRow("SELECT author,kind,undone FROM actions WHERE id=?", op.Action).Scan(&author, &kind, &undone)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.Exec("INSERT INTO actions(id,author,kind,revision) VALUES (?,?,?,?)", op.Action, op.Author, op.Kind, revision)
		} else if err == nil && (author != op.Author || kind != op.Kind || undone != 0) {
			return change{}, errInvalid
		}
		if err != nil {
			return change{}, err
		}
		if op.Kind == "pen" {
			err = insertStroke(tx, op)
			ids = []string{op.ID}
		} else {
			ids, err = eraseStrokes(tx, op)
		}
		if err != nil {
			return change{}, err
		}
	}
	// Keep completely erased strokes out of spatial loads without deleting undo data.
	if op.Kind != "pen" {
		for _, id := range ids {
			var stroke storedStroke
			var raw []byte
			if err = tx.QueryRow("SELECT id,width,points FROM strokes WHERE id=?", id).Scan(&stroke.id, &stroke.width, &raw); err != nil {
				return change{}, err
			}
			if err = json.Unmarshal(raw, &stroke.points); err != nil {
				return change{}, err
			}
			d, e := renderStroke(tx, stroke)
			if e != nil {
				return change{}, e
			}
			if _, err = tx.Exec("UPDATE strokes SET visible=? WHERE id=?", len(d.Lines) > 0, id); err != nil {
				return change{}, err
			}
		}
	}
	if _, err = tx.Exec("INSERT INTO receipts VALUES (?,?,?)", op.ID, digest[:], revision); err != nil {
		return change{}, err
	}
	if err = tx.Commit(); err != nil {
		return change{}, err
	}
	return change{Revision: revision, IDs: ids}, nil
}
func insertStroke(tx *sql.Tx, op edit) error {
	b := pathBounds(op.Points, op.Width/2)
	data, _ := json.Marshal(op.Points)
	if _, err := tx.Exec("INSERT INTO strokes(id,action,width,points,x0,y0,x1,y1) VALUES (?,?,?,?,?,?,?,?)", op.ID, op.Action, op.Width, data, b[0], b[1], b[2], b[3]); err != nil {
		return err
	}
	stmt, err := tx.Prepare("INSERT INTO cells VALUES (?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for x := int(math.Floor(b[0] / cellSize)); x <= int(math.Floor(b[2]/cellSize)); x++ {
		for y := int(math.Floor(b[1] / cellSize)); y <= int(math.Floor(b[3]/cellSize)); y++ {
			if _, err = stmt.Exec(x, y, op.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func queryStrokes(tx *sql.Tx, b bounds) ([]storedStroke, error) {
	rows, err := tx.Query(`SELECT DISTINCT s.id,s.width,s.points,s.x0,s.y0,s.x1,s.y1 FROM cells c JOIN strokes s ON s.id=c.stroke JOIN actions a ON a.id=s.action WHERE c.x BETWEEN ? AND ? AND c.y BETWEEN ? AND ? AND a.undone=0 AND s.visible=1 AND s.x0<=? AND s.x1>=? AND s.y0<=? AND s.y1>=? ORDER BY s.id LIMIT ?`, int(math.Floor(b[0]/cellSize)), int(math.Floor(b[2]/cellSize)), int(math.Floor(b[1]/cellSize)), int(math.Floor(b[3]/cellSize)), b[2], b[0], b[3], b[1], maxViewStrokes+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var strokes []storedStroke
	for rows.Next() {
		var s storedStroke
		var raw []byte
		if err = rows.Scan(&s.id, &s.width, &raw, &s.area[0], &s.area[1], &s.area[2], &s.area[3]); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &s.points); err != nil {
			return nil, err
		}
		strokes = append(strokes, s)
	}
	if len(strokes) > maxViewStrokes {
		return nil, errDense
	}
	return strokes, rows.Err()
}
func eraseStrokes(tx *sql.Tx, op edit) ([]string, error) {
	strokes, err := queryStrokes(tx, pathBounds(op.Points, op.Width/2))
	if err != nil {
		return nil, err
	}
	erasers := pathLines(op.Points)
	ids := []string{}
	budget := 2_000_000
	stmt, err := tx.Prepare("INSERT OR IGNORE INTO cuts VALUES (?,?,?,?,?)")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for _, stroke := range strokes {
		changed := false
		for i, l := range pathLines(stroke.points) {
			var cuts []interval
			for _, eraser := range erasers {
				budget--
				if budget < 0 {
					return nil, errDense
				}
				cuts = append(cuts, capsuleCut(l, eraser, (op.Width+stroke.width)/2)...)
			}
			for _, cut := range mergeIntervals(cuts) {
				if _, err = stmt.Exec(stroke.id, op.Action, i, cut[0], cut[1]); err != nil {
					return nil, err
				}
				changed = true
			}
		}
		if changed {
			ids = append(ids, stroke.id)
		}
	}
	return ids, nil
}
func renderStroke(tx *sql.Tx, s storedStroke) (drawing, error) {
	d := drawing{ID: s.id, Width: s.width, Lines: []line{}}
	rows, err := tx.Query("SELECT c.segment,c.lo,c.hi FROM cuts c JOIN actions a ON a.id=c.eraser WHERE c.stroke=? AND a.undone=0 LIMIT ?", s.id, maxViewLines+1)
	if err != nil {
		return d, err
	}
	cuts := map[int][]interval{}
	count := 0
	for rows.Next() {
		var i int
		var c interval
		if err = rows.Scan(&i, &c[0], &c[1]); err != nil {
			rows.Close()
			return d, err
		}
		cuts[i] = append(cuts[i], c)
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	if count > maxViewLines {
		return d, errDense
	}
	for i, l := range pathLines(s.points) {
		d.Lines = append(d.Lines, visibleLines(l, cuts[i])...)
	}
	return d, nil
}

func (s *boardStore) View(area bounds) ([]drawing, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !area.valid(maxViewSpan) {
		return nil, 0, errInvalid
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRow("SELECT revision FROM board").Scan(&revision); err != nil {
		return nil, 0, err
	}
	strokes, err := queryStrokes(tx, area)
	if err != nil {
		return nil, 0, err
	}
	drawings := []drawing{}
	count := 0
	for _, stroke := range strokes {
		d, e := renderStroke(tx, stroke)
		if e != nil {
			return nil, 0, e
		}
		count += len(d.Lines)
		if count > maxViewLines {
			return nil, 0, errDense
		}
		if len(d.Lines) > 0 {
			drawings = append(drawings, d)
		}
	}
	return drawings, revision, nil
}

// Missing/undone/out-of-view strokes are represented by empty line lists.
func (s *boardStore) Patch(ids []string, area bounds) ([]drawing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := []drawing{}
	count := 0
	for _, id := range ids {
		var stroke storedStroke
		var raw []byte
		err = tx.QueryRow("SELECT s.id,s.width,s.points,s.x0,s.y0,s.x1,s.y1 FROM strokes s JOIN actions a ON a.id=s.action WHERE s.id=? AND a.undone=0", id).Scan(&stroke.id, &stroke.width, &raw, &stroke.area[0], &stroke.area[1], &stroke.area[2], &stroke.area[3])
		if errors.Is(err, sql.ErrNoRows) || err == nil && !stroke.area.overlaps(area) {
			result = append(result, drawing{ID: id, Width: 1, Lines: []line{}})
			continue
		}
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &stroke.points); err != nil {
			return nil, err
		}
		d, e := renderStroke(tx, stroke)
		if e != nil {
			return nil, e
		}
		count += len(d.Lines)
		if count > maxViewLines {
			return nil, errDense
		}
		result = append(result, d)
	}
	return result, nil
}
func (s *boardStore) History(author string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT id FROM actions WHERE author=? AND undone=0 ORDER BY revision DESC LIMIT 50", author)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// VACUUM INTO reads a consistent live snapshot. Publish without replacing a file.
func (s *boardStore) Backup(destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.CreateTemp(filepath.Dir(destination), ".bukupan-backup-*")
	if err != nil {
		return err
	}
	name := file.Name()
	file.Close()
	defer os.Remove(name)
	if _, err = s.db.Exec("VACUUM INTO ?", name); err != nil {
		return err
	}
	file, err = os.Open(name)
	if err != nil {
		return err
	}
	err = file.Sync()
	file.Close()
	if err != nil {
		return err
	}
	if err = os.Link(name, destination); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

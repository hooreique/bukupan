package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testBoard(t *testing.T) *boardStore {
	t.Helper()
	s, err := openBoard(filepath.Join(t.TempDir(), "board.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func pen(author string, points ...point) edit {
	return edit{ID: randomID(), Author: author, Action: randomID(), Kind: "pen", Width: 2, Points: points}
}
func applyEdit(t *testing.T, s *boardStore, op edit) change {
	t.Helper()
	c, err := s.Apply(op)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func boardView(t *testing.T, s *boardStore, b bounds) []drawing {
	t.Helper()
	d, _, err := s.View(b)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBoardDurabilityAndDeduplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.sqlite")
	s, err := openBoard(path)
	if err != nil {
		t.Fatal(err)
	}
	op := pen(randomID(), point{-10, 0}, point{10, 0})
	first := applyEdit(t, s, op)
	id := s.ID
	s.Close()
	s, err = openBoard(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.ID != id {
		t.Fatal("document identity changed")
	}
	again := applyEdit(t, s, op)
	if !again.Duplicate || again.Revision != first.Revision {
		t.Fatal("retry was not idempotent")
	}
	view, rev, err := s.View(bounds{-20, -20, 20, 20})
	if err != nil || rev != 1 || len(view) != 1 {
		t.Fatalf("restore: %v %d %v", view, rev, err)
	}
	op.Points[0][0]++
	if _, err = s.Apply(op); !errors.Is(err, errInvalid) {
		t.Fatal("accepted reused ID with changed payload")
	}
}

func TestPartialEraseAndIndependentUndo(t *testing.T) {
	s := testBoard(t)
	a, b := randomID(), randomID()
	original := pen(a, point{-100, 0}, point{100, 0})
	applyEdit(t, s, original)
	eraser := edit{ID: randomID(), Author: a, Action: randomID(), Kind: "erase", Width: 18, Points: []point{{0, -30}, {0, 30}}}
	applyEdit(t, s, eraser)
	view := boardView(t, s, bounds{-200, -100, 200, 100})
	if len(view) != 1 || len(view[0].Lines) != 2 || math.Abs(view[0].Lines[0][2]+10) > 1e-9 || math.Abs(view[0].Lines[1][0]-10) > 1e-9 {
		t.Fatalf("partial erasure: %+v", view)
	}
	other := pen(b, point{0, -50}, point{0, 50})
	applyEdit(t, s, other)
	undo := edit{ID: randomID(), Author: a, Kind: "undo", Target: eraser.Action}
	applyEdit(t, s, undo)
	view = boardView(t, s, bounds{-200, -100, 200, 100})
	if len(view) != 2 {
		t.Fatal("undo lost another participant's stroke")
	}
	for _, d := range view {
		if len(d.Lines) != 1 {
			t.Fatal("undo did not restore the original geometry")
		}
	}
	applyEdit(t, s, edit{ID: randomID(), Author: a, Kind: "undo", Target: original.Action})
	view = boardView(t, s, bounds{-200, -100, 200, 100})
	if len(view) != 1 || view[0].ID != other.ID {
		t.Fatal("undo drawing removed another user's work")
	}
	if _, err := s.Apply(edit{ID: randomID(), Author: a, Kind: "undo", Target: other.Action}); !errors.Is(err, errInvalid) {
		t.Fatal("undid someone else's action")
	}
}

func TestEraseUndoPreservesOtherErasers(t *testing.T) {
	s := testBoard(t)
	a, b := randomID(), randomID()
	applyEdit(t, s, pen(a, point{-100, 0}, point{100, 0}))
	first := edit{ID: randomID(), Author: a, Action: randomID(), Kind: "erase", Width: 18, Points: []point{{-50, 0}}}
	second := edit{ID: randomID(), Author: b, Action: randomID(), Kind: "erase", Width: 18, Points: []point{{50, 0}}}
	applyEdit(t, s, first)
	applyEdit(t, s, second)
	applyEdit(t, s, edit{ID: randomID(), Author: a, Kind: "undo", Target: first.Action})
	view := boardView(t, s, bounds{-200, -100, 200, 100})
	if len(view[0].Lines) != 2 || view[0].Lines[0][2] != 40 || view[0].Lines[1][0] != 60 {
		t.Fatalf("other eraser lost: %+v", view)
	}
}

func TestActionGroupsAndSpatialQueries(t *testing.T) {
	s := testBoard(t)
	author := randomID()
	a := pen(author, point{-1025, 0}, point{-1023, 0})
	b := pen(author, point{1e8, 1e8}, point{1e8 + 1, 1e8 + 1})
	b.Action = a.Action
	applyEdit(t, s, a)
	applyEdit(t, s, b)
	if len(boardView(t, s, bounds{-1024, -10, -1020, 10})) != 1 {
		t.Fatal("negative tile edge missing")
	}
	if got := boardView(t, s, bounds{1e8 - 2, 1e8 - 2, 1e8 + 2, 1e8 + 2}); len(got) != 1 || got[0].Lines[0][2]-got[0].Lines[0][0] != 1 {
		t.Fatal("distant stroke precision lost")
	}
	history, err := s.History(author)
	if err != nil || len(history) != 1 {
		t.Fatal("chunks should be one action", err)
	}
	c := applyEdit(t, s, edit{ID: randomID(), Author: author, Kind: "undo", Target: a.Action})
	if len(c.IDs) != 2 {
		t.Fatal("undo did not cover the whole gesture")
	}
	if len(boardView(t, s, bounds{-1030, -10, -1020, 10})) != 0 {
		t.Fatal("undone stroke still visible")
	}
}

func TestFailedWriteRollsBackRevisionAndReceipt(t *testing.T) {
	s := testBoard(t)
	op := pen(randomID(), point{0, 0})
	if _, err := s.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(op); err == nil {
		t.Fatal("read-only storage accepted edit")
	}
	if _, err := s.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	c := applyEdit(t, s, op)
	if c.Revision != 1 || c.Duplicate {
		t.Fatal("failed write left a receipt/revision")
	}
}

func TestLiveBackupAndUnknownSchema(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "board.sqlite")
	s, err := openBoard(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	applyEdit(t, s, pen(randomID(), point{5, 5}))
	destination := filepath.Join(dir, "backup.sqlite")
	if err = s.Backup(destination); err != nil {
		t.Fatal(err)
	}
	if err = s.Backup(destination); err == nil {
		t.Fatal("backup replaced an existing file")
	}
	applyEdit(t, s, pen(randomID(), point{6, 6}))
	restored, err := openBoard(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if len(boardView(t, restored, bounds{0, 0, 10, 10})) != 1 || len(boardView(t, s, bounds{0, 0, 10, 10})) != 2 {
		t.Fatal("backup not a separate snapshot")
	}
	if _, err = s.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	if other, err := openBoard(filename); err == nil {
		other.Close()
		t.Fatal("accepted future schema")
	}
}

func TestConcurrentBoardEdits(t *testing.T) {
	s := testBoard(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Apply(pen(randomID(), point{0, 0})); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	d, revision, err := s.View(bounds{-10, -10, 10, 10})
	if err != nil || len(d) != 16 || revision != 16 {
		t.Fatal("concurrent edits lost", len(d), revision, err)
	}
}

func TestFullyErasedStrokeReturnsOnUndo(t *testing.T) {
	s := testBoard(t)
	author := randomID()
	op := pen(author, point{0, 0})
	applyEdit(t, s, op)
	erase := edit{ID: randomID(), Author: author, Action: randomID(), Kind: "erase", Width: 20, Points: []point{{0, 0}}}
	applyEdit(t, s, erase)
	if len(boardView(t, s, bounds{-10, -10, 10, 10})) != 0 {
		t.Fatal("erased dot visible")
	}
	applyEdit(t, s, edit{ID: randomID(), Author: author, Kind: "undo", Target: erase.Action})
	if len(boardView(t, s, bounds{-10, -10, 10, 10})) != 1 {
		t.Fatal("undo did not restore spatial visibility")
	}
}

func TestErasingEmptySpaceDoesNotHideTheLastUndo(t *testing.T) {
	s := testBoard(t)
	author := randomID()
	stroke := pen(author, point{0, 0})
	applyEdit(t, s, stroke)
	applyEdit(t, s, edit{ID: randomID(), Author: author, Action: randomID(), Kind: "erase", Width: 20, Points: []point{{100, 100}}})
	history, err := s.History(author)
	if err != nil || len(history) != 1 || history[0] != stroke.Action {
		t.Fatalf("empty eraser entered undo history: %v %v", history, err)
	}
}

func TestDenseViewKeepsDataAndCanLoadASmallerArea(t *testing.T) {
	s := testBoard(t)
	author := randomID()
	op := pen(author, point{0, 0})
	applyEdit(t, s, op)
	// Seed one dense gesture in a fixture transaction. Per-edit durability is
	// covered separately; this test concerns bounded reads without eviction.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= maxViewStrokes; i++ {
		op.ID = randomID()
		op.Points = []point{{float64(i), 0}}
		if err = insertStroke(tx, op); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.View(bounds{-10, -10, float64(maxViewStrokes + 10), 10}); !errors.Is(err, errDense) {
		t.Fatalf("unbounded view accepted: %v", err)
	}
	drawings := boardView(t, s, bounds{-1, -1, 1, 1})
	if len(drawings) == 0 || len(drawings) > 5 {
		t.Fatalf("smaller area not isolated: %d", len(drawings))
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM strokes").Scan(&count); err != nil || count != maxViewStrokes+1 {
		t.Fatalf("view limit discarded drawings: %d %v", count, err)
	}
}

func TestStoreRejectsUnboundedInput(t *testing.T) {
	s := testBoard(t)
	for _, op := range []edit{
		pen(randomID(), point{math.NaN(), 0}),
		pen(randomID(), point{worldLimit, 0}),
		pen(randomID(), point{0, 0}, point{maxPathSpan + 1, 0}),
		pen(randomID(), make([]point, 129)...),
	} {
		if _, err := s.Apply(op); !errors.Is(err, errInvalid) {
			t.Fatalf("accepted invalid input: %v", err)
		}
	}
	if _, _, err := s.View(bounds{-worldLimit, 0, worldLimit, 100}); !errors.Is(err, errInvalid) {
		t.Fatal("unbounded viewport accepted")
	}
}

func TestCommittedDrawingSurvivesProcessKill(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "board.sqlite")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBoardCrashHelper$")
	child.Env = append(os.Environ(), "BUKUPAN_CRASH_TEST="+filename)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "committed" {
		t.Fatal("child did not confirm commit", scanner.Text())
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	s, err := openBoard(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(boardView(t, s, bounds{-10, -10, 10, 10})) != 1 {
		t.Fatal("confirmed drawing lost after kill")
	}
}
func TestBoardCrashHelper(t *testing.T) {
	filename := os.Getenv("BUKUPAN_CRASH_TEST")
	if filename == "" {
		return
	}
	s, err := openBoard(filename)
	if err != nil {
		t.Fatal(err)
	}
	applyEdit(t, s, pen(randomID(), point{0, 0}))
	fmt.Println("committed")
	for {
		time.Sleep(time.Hour)
	}
}

func TestSweptEraserGeometry(t *testing.T) {
	cases := []struct {
		line   line
		erase  line
		radius float64
		want   []interval
	}{
		{line{-100, 0, 100, 0}, line{0, -20, 0, 20}, 10, []interval{{.45, .55}}},
		{line{-100, 0, 100, 0}, line{-20, 0, 20, 0}, 10, []interval{{.35, .65}}},
		{line{0, 0, 0, 0}, line{-10, 0, 10, 0}, 1, []interval{{0, 1}}},
		{line{0, 10, 0, 10}, line{-10, 0, 10, 0}, 1, []interval{}},
	}
	for _, c := range cases {
		got := capsuleCut(c.line, c.erase, c.radius)
		if len(got) != len(c.want) {
			t.Fatalf("got %v want %v", got, c.want)
		}
		for i := range got {
			for j := range got[i] {
				if math.Abs(got[i][j]-c.want[i][j]) > 1e-10 {
					t.Fatalf("got %v want %v", got, c.want)
				}
			}
		}
	}
}

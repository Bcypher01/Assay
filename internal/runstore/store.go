package runstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	_ "modernc.org/sqlite"

	"github.com/bcypher01/assay/internal/analytics"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/sim"
)

var ErrNotFound = errors.New("runstore: run not found")

type RunID string
type Scope = analytics.Scope

// Filter restricts a history listing. Empty fields are ignored. Sort accepts
// "date", "metric", or "instrument"; descending is the default for date and
// metric, ascending for instrument.
type Filter struct {
	Instrument, Sort string
	Desc             bool
}

type RunSummary struct {
	ID               RunID             `json:"id"`
	StrategyHash     string            `json:"strategyHash"`
	Snapshot         string            `json:"snapshot"`
	Name             string            `json:"name"`
	Scope            Scope             `json:"scope"`
	CreatedAt        time.Time         `json:"createdAt"`
	Trades           int               `json:"trades"`
	FinalEquity      analytics.Bounded `json:"finalEquity"`
	Parameters       string            `json:"parameters"`
	TradeListPresent bool              `json:"tradeListPresent"`
}

type Run struct {
	RunSummary
	Study  *analytics.Study       `json:"study"`
	Result *analytics.StudyResult `json:"result"`
	Trades []sim.Trade            `json:"trades,omitempty"`
}
type ParamChange struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
}
type Diff struct {
	ParamChanges []ParamChange                `json:"paramChanges"`
	MetricDeltas map[string]analytics.Bounded `json:"metricDeltas"`
}

type Store interface {
	Put(context.Context, *analytics.Study, *analytics.StudyResult) (RunID, error)
	Get(context.Context, RunID) (*Run, error)
	List(context.Context, Filter) ([]RunSummary, error)
	Trials(context.Context, Scope) (int, error)
	Compare(context.Context, RunID, RunID) (Diff, error)
	Export(context.Context, RunID, io.Writer) error
}

// LocalStore keeps queryable run history in SQLite and stores the larger,
// immutable study, result, and trade artifacts as files beside it.
type LocalStore struct {
	root     string
	maxBytes int64
	mu       sync.Mutex
	db       *sql.DB
}
type Option func(*LocalStore)

func WithMaxBytes(n int64) Option { return func(s *LocalStore) { s.maxBytes = n } }
func Open(root string, options ...Option) (*LocalStore, error) {
	s := &LocalStore{root: root, maxBytes: 512 << 20}
	for _, option := range options {
		option(s)
	}
	if err := os.MkdirAll(filepath.Join(root, "runs"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "history.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s.db = db
	if err := s.initDB(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrateLegacyIndex(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}
func New(root string, options ...Option) (*LocalStore, error) { return Open(root, options...) }
func (s *LocalStore) indexPath() string                       { return filepath.Join(s.root, "index.json") }
func (s *LocalStore) runDir(id RunID) string                  { return filepath.Join(s.root, "runs", string(id)) }

func (s *LocalStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *LocalStore) initDB() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			strategy_hash TEXT NOT NULL,
			snapshot TEXT NOT NULL,
			name TEXT NOT NULL,
			scope_json TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			trades INTEGER NOT NULL,
			final_equity_pess REAL NOT NULL,
			final_equity_opt REAL NOT NULL,
			parameters TEXT NOT NULL,
			trade_list_present INTEGER NOT NULL DEFAULT 0,
			study_json BLOB,
			result_json BLOB
		);
		CREATE INDEX IF NOT EXISTS runs_created_at ON runs(created_at_ms DESC);
		CREATE INDEX IF NOT EXISTS runs_strategy_scope ON runs(strategy_hash, scope_json);
	`)
	if err != nil {
		return err
	}
	return s.ensureDocumentColumns()
}

func (s *LocalStore) ensureDocumentColumns() error {
	rows, err := s.db.Query("PRAGMA table_info(runs)")
	if err != nil {
		return err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		columns[name] = true
	}
	for _, column := range []string{"study_json", "result_json"} {
		if !columns[column] {
			if _, err := s.db.Exec("ALTER TABLE runs ADD COLUMN " + column + " BLOB"); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

// migrateLegacyIndex imports the former JSON index once, without deleting it.
// The original run documents remain intact, so an interrupted upgrade is safe
// to retry and users retain a straightforward recovery path.
func (s *LocalStore) migrateLegacyIndex() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM runs").Scan(&count); err != nil || count != 0 {
		return err
	}
	items, err := s.readLegacyIndex()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		study, result, err := s.legacyDocuments(item.ID)
		if err != nil {
			return err
		}
		if err := s.insertRun(tx, item, study, result); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *LocalStore) Put(ctx context.Context, study *analytics.Study, result *analytics.StudyResult) (RunID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if study == nil || study.Strategy == nil || result == nil {
		return "", fmt.Errorf("runstore: study, strategy, and result are required")
	}
	strategyHash := study.Strategy.Hash()
	snapshot := study.Snapshot
	if snapshot == "" {
		return "", fmt.Errorf("runstore: data snapshot is required")
	}
	id := runID(strategyHash, snapshot)
	s.mu.Lock()
	defer s.mu.Unlock()
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM runs WHERE id = ?", string(id)).Scan(&exists)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := s.runDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	studyJSON, err := json.Marshal(study)
	if err != nil {
		return "", err
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	summary := makeSummary(id, strategyHash, study, result)
	if err := s.insertRun(s.db, summary, studyJSON, resultJSON); err != nil {
		return "", err
	}
	if err := s.enforceRetention(); err != nil {
		return "", err
	}
	return id, nil
}

// PutTrades writes the full simulator trade list after Put. Keeping this
// separate lets analytics be persisted before a large list arrives.
func (s *LocalStore) PutTrades(ctx context.Context, id RunID, trades []sim.Trade) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM runs WHERE id = ?", string(id)).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := writeTrades(filepath.Join(s.runDir(id), "trades.parquet"), trades); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE runs SET trade_list_present = 1 WHERE id = ?", string(id)); err != nil {
		return err
	}
	return s.enforceRetention()
}
func (s *LocalStore) Get(ctx context.Context, id RunID) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	summary, err := s.summaryByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	studyJSON, resultJSON, err := s.documentsByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(studyJSON) == 0 || len(resultJSON) == 0 {
		studyJSON, resultJSON, err = s.legacyDocuments(id)
		if err != nil {
			return nil, err
		}
	}
	r := &Run{RunSummary: summary, Study: &analytics.Study{}, Result: &analytics.StudyResult{}}
	if err := json.Unmarshal(studyJSON, r.Study); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(resultJSON, r.Result); err != nil {
		return nil, err
	}
	if summary.TradeListPresent {
		r.Trades, _ = readTrades(filepath.Join(s.runDir(id), "trades.parquet"))
	}
	return r, nil
}
func (s *LocalStore) List(ctx context.Context, f Filter) ([]RunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listSummaries(ctx)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, item := range items {
		if f.Instrument == "" || contains(item.Scope.Instruments, f.Instrument) {
			out = append(out, item)
		}
	}
	desc := f.Desc || f.Sort == "" || f.Sort == "date"
	sort.Slice(out, func(i, j int) bool {
		var less bool
		switch f.Sort {
		case "instrument":
			less = firstInstrument(out[i]) < firstInstrument(out[j])
		case "metric":
			less = out[i].FinalEquity.Pessimistic < out[j].FinalEquity.Pessimistic
		default:
			less = out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		if desc {
			return !less && out[i].ID != out[j].ID
		}
		return less
	})
	return append([]RunSummary(nil), out...), nil
}
func (s *LocalStore) Trials(ctx context.Context, scope Scope) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.listSummaries(ctx)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	key := scopeKey(scope)
	for _, item := range items {
		if scopeKey(item.Scope) == key {
			seen[item.StrategyHash] = true
		}
	}
	return len(seen), nil
}
func (s *LocalStore) Compare(ctx context.Context, a, b RunID) (Diff, error) {
	left, e := s.Get(ctx, a)
	if e != nil {
		return Diff{}, e
	}
	right, e := s.Get(ctx, b)
	if e != nil {
		return Diff{}, e
	}
	return compare(left, right), nil
}
func (s *LocalStore) Export(ctx context.Context, id RunID, w io.Writer) error {
	r, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(r)
}

func makeSummary(id RunID, hash string, s *analytics.Study, r *analytics.StudyResult) RunSummary {
	//nolint:forbidigo // CreatedAt stamps the history record, not the run's numbers
	return RunSummary{ID: id, StrategyHash: hash, Snapshot: s.Snapshot, Name: s.Strategy.Name, Scope: canonicalScope(s.Scope, s.Strategy.Universe.Symbols), CreatedAt: time.Now().UTC(), Trades: r.OutOfSample.Trades, FinalEquity: r.OutOfSample.FinalEquity, Parameters: paramSummary(s.Strategy)}
}
func runID(strategy, snapshot string) RunID {
	h := sha256.Sum256([]byte(strategy + "\x00" + snapshot))
	return RunID(hex.EncodeToString(h[:]))
}
func canonicalScope(s Scope, fallback []string) Scope {
	if len(s.Instruments) == 0 {
		s.Instruments = append([]string(nil), fallback...)
	}
	sort.Strings(s.Instruments)
	return s
}
func scopeKey(s Scope) string {
	s = canonicalScope(s, nil)
	return strings.Join(s.Instruments, "\x1f") + "\x1e" + s.Period
}
func contains(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}
func firstInstrument(s RunSummary) string {
	if len(s.Scope.Instruments) == 0 {
		return ""
	}
	return s.Scope.Instruments[0]
}
func paramSummary(s *ir.Strategy) string {
	if s == nil {
		return ""
	}
	var p []string
	for _, n := range s.Nodes {
		p = append(p, n.ID+"="+string(n.Params))
	}
	sort.Strings(p)
	return strings.Join(p, ", ")
}
func compare(a, b *Run) Diff {
	d := Diff{MetricDeltas: map[string]analytics.Bounded{}}
	am, bm := strategyParams(a.Study.Strategy), strategyParams(b.Study.Strategy)
	keys := map[string]bool{}
	for k := range am {
		keys[k] = true
	}
	for k := range bm {
		keys[k] = true
	}
	for k := range keys {
		if am[k] != bm[k] {
			d.ParamChanges = append(d.ParamChanges, ParamChange{Path: k, From: am[k], To: bm[k]})
		}
	}
	sort.Slice(d.ParamChanges, func(i, j int) bool { return d.ParamChanges[i].Path < d.ParamChanges[j].Path })
	d.MetricDeltas["outOfSample.finalEquity"] = delta(a.Result.OutOfSample.FinalEquity, b.Result.OutOfSample.FinalEquity)
	d.MetricDeltas["outOfSample.maxDrawdown"] = delta(a.Result.OutOfSample.MaxDrawdown, b.Result.OutOfSample.MaxDrawdown)
	d.MetricDeltas["outOfSample.expectancy"] = delta(a.Result.OutOfSample.Expectancy, b.Result.OutOfSample.Expectancy)
	return d
}
func strategyParams(s *ir.Strategy) map[string]string {
	m := map[string]string{}
	if s == nil {
		return m
	}
	m["risk.stop"] = fmt.Sprint(s.Risk.Stop)
	m["risk.target"] = fmt.Sprint(s.Risk.Target)
	m["risk.perTrade"] = fmt.Sprint(s.Risk.PerTrade)
	m["direction"] = string(s.Direction)
	for _, n := range s.Nodes {
		m["nodes."+n.ID+".params"] = string(n.Params)
	}
	return m
}
func delta(a, b analytics.Bounded) analytics.Bounded {
	return analytics.Bounded{Pessimistic: b.Pessimistic - a.Pessimistic, Optimistic: b.Optimistic - a.Optimistic}
}
func (s *LocalStore) readLegacyIndex() ([]RunSummary, error) {
	var out []RunSummary
	err := readJSON(s.indexPath(), &out)
	return out, err
}
func (s *LocalStore) insertRun(exec interface {
	Exec(string, ...any) (sql.Result, error)
}, item RunSummary, study, result []byte) error {
	scope, err := json.Marshal(canonicalScope(item.Scope, nil))
	if err != nil {
		return err
	}
	_, err = exec.Exec(`INSERT OR IGNORE INTO runs (
		id, strategy_hash, snapshot, name, scope_json, created_at_ms, trades,
		final_equity_pess, final_equity_opt, parameters, trade_list_present, study_json, result_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(item.ID), item.StrategyHash, item.Snapshot, item.Name, string(scope), item.CreatedAt.UnixMilli(), item.Trades,
		item.FinalEquity.Pessimistic, item.FinalEquity.Optimistic, item.Parameters, item.TradeListPresent, study, result)
	return err
}

func (s *LocalStore) legacyDocuments(id RunID) ([]byte, []byte, error) {
	study, err := os.ReadFile(filepath.Join(s.runDir(id), "study.json"))
	if err != nil {
		return nil, nil, err
	}
	result, err := os.ReadFile(filepath.Join(s.runDir(id), "result.json"))
	if err != nil {
		return nil, nil, err
	}
	return study, result, nil
}

func (s *LocalStore) listSummaries(ctx context.Context) ([]RunSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, strategy_hash, snapshot, name, scope_json, created_at_ms, trades,
		final_equity_pess, final_equity_opt, parameters, trade_list_present FROM runs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunSummary
	for rows.Next() {
		item, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *LocalStore) summaryByID(ctx context.Context, id RunID) (RunSummary, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, strategy_hash, snapshot, name, scope_json, created_at_ms, trades,
		final_equity_pess, final_equity_opt, parameters, trade_list_present FROM runs WHERE id = ?`, string(id))
	return scanSummary(row)
}

func (s *LocalStore) documentsByID(ctx context.Context, id RunID) ([]byte, []byte, error) {
	var study, result []byte
	err := s.db.QueryRowContext(ctx, "SELECT study_json, result_json FROM runs WHERE id = ?", string(id)).Scan(&study, &result)
	return study, result, err
}

type summaryScanner interface{ Scan(...any) error }

func scanSummary(scanner summaryScanner) (RunSummary, error) {
	var item RunSummary
	var scope string
	var created int64
	var present int
	err := scanner.Scan(&item.ID, &item.StrategyHash, &item.Snapshot, &item.Name, &scope, &created, &item.Trades,
		&item.FinalEquity.Pessimistic, &item.FinalEquity.Optimistic, &item.Parameters, &present)
	if err != nil {
		return RunSummary{}, err
	}
	if err := json.Unmarshal([]byte(scope), &item.Scope); err != nil {
		return RunSummary{}, err
	}
	item.CreatedAt = time.UnixMilli(created).UTC()
	item.TradeListPresent = present != 0
	return item, nil
}

func (s *LocalStore) enforceRetention() error {
	if s.maxBytes <= 0 {
		return nil
	}
	items, err := s.listSummaries(context.Background())
	if err != nil {
		return err
	}
	var size int64
	type victim struct {
		id RunID
		at time.Time
	}
	var candidates []victim
	for _, i := range items {
		p := filepath.Join(s.runDir(i.ID), "trades.parquet")
		if x, e := os.Stat(p); e == nil {
			size += x.Size()
			candidates = append(candidates, victim{i.ID, i.CreatedAt})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.Before(candidates[j].at) })
	for _, v := range candidates {
		if size <= s.maxBytes {
			break
		}
		p := filepath.Join(s.runDir(v.id), "trades.parquet")
		x, e := os.Stat(p)
		if e != nil {
			continue
		}
		if e = os.Remove(p); e != nil {
			return e
		}
		size -= x.Size()
		for i := range items {
			if items[i].ID == v.id {
				items[i].TradeListPresent = false
				if _, err := s.db.Exec("UPDATE runs SET trade_list_present = 0 WHERE id = ?", string(v.id)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(path, append(b, '\n'))
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func atomicWrite(path string, b []byte) error {
	tmp := path + ".tmp"
	if e := os.WriteFile(tmp, b, 0o644); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

type tradeRow struct {
	Side          int32 `parquet:"side"`
	EntryTime     int64 `parquet:"entry_time"`
	EntryBar      int64 `parquet:"entry_bar"`
	EntryPrice    int64 `parquet:"entry_price"`
	Stop          int64 `parquet:"stop"`
	Target        int64 `parquet:"target"`
	Lots          int64 `parquet:"lots"`
	PessNet       int64 `parquet:"pess_net"`
	OptNet        int64 `parquet:"opt_net"`
	Ambiguous     bool  `parquet:"ambiguous"`
	PessExitTime  int64 `parquet:"pess_exit_time"`
	OptExitTime   int64 `parquet:"opt_exit_time"`
	PessExitPrice int64 `parquet:"pess_exit_price"`
	OptExitPrice  int64 `parquet:"opt_exit_price"`
	PessCosts     int64 `parquet:"pess_costs"`
	OptCosts      int64 `parquet:"opt_costs"`
}

func writeTrades(path string, trades []sim.Trade) error {
	var b bytes.Buffer
	w := parquet.NewGenericWriter[tradeRow](&b)
	rows := make([]tradeRow, len(trades))
	for i, t := range trades {
		rows[i] = tradeRow{Side: int32(t.Side), EntryTime: int64(t.EntryTime), EntryBar: int64(t.EntryBar), EntryPrice: int64(t.EntryPrice), Stop: int64(t.Stop), Target: int64(t.Target), Lots: int64(t.Lots), PessNet: int64(t.Pessimistic.Net), OptNet: int64(t.Optimistic.Net), Ambiguous: t.Ambiguous, PessExitTime: int64(t.Pessimistic.ExitTime), OptExitTime: int64(t.Optimistic.ExitTime), PessExitPrice: int64(t.Pessimistic.ExitPrice), OptExitPrice: int64(t.Optimistic.ExitPrice), PessCosts: int64(t.Pessimistic.Costs), OptCosts: int64(t.Optimistic.Costs)}
	}
	if _, e := w.Write(rows); e != nil {
		return e
	}
	if e := w.Close(); e != nil {
		return e
	}
	return atomicWrite(path, b.Bytes())
}
func readTrades(path string) ([]sim.Trade, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := parquet.NewGenericReader[tradeRow](f)
	defer func() { _ = r.Close() }()
	var rows []tradeRow
	buf := make([]tradeRow, 256)
	for {
		n, e := r.Read(buf)
		rows = append(rows, buf[:n]...)
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	out := make([]sim.Trade, len(rows))
	for i, x := range rows {
		out[i] = sim.Trade{Side: core.Side(x.Side), EntryTime: core.Time(x.EntryTime), EntryBar: int(x.EntryBar), EntryPrice: core.Price(x.EntryPrice), Stop: core.Price(x.Stop), Target: core.Price(x.Target), Lots: core.Ratio(x.Lots), Pessimistic: sim.Outcome{ExitTime: core.Time(x.PessExitTime), ExitPrice: core.Price(x.PessExitPrice), Net: core.Price(x.PessNet), Costs: core.Price(x.PessCosts)}, Optimistic: sim.Outcome{ExitTime: core.Time(x.OptExitTime), ExitPrice: core.Price(x.OptExitPrice), Net: core.Price(x.OptNet), Costs: core.Price(x.OptCosts)}, Ambiguous: x.Ambiguous}
	}
	return out, nil
}

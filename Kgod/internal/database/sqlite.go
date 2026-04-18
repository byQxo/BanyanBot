package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Candle 是本地持久化 K 线实体。
type Candle struct {
	Symbol    string
	Timeframe string
	OpenTime  int64
	CloseTime int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Turnover  float64
}

// SQLiteStore 封装本地 SQLite 持久化层。
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore 初始化 SQLite 并创建行情表。
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite failed: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) initSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ddl := `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS candles (
	symbol TEXT NOT NULL,
	timeframe TEXT NOT NULL,
	open_time INTEGER NOT NULL,
	close_time INTEGER NOT NULL,
	open REAL NOT NULL,
	high REAL NOT NULL,
	low REAL NOT NULL,
	close REAL NOT NULL,
	volume REAL NOT NULL,
	turnover REAL NOT NULL,
	PRIMARY KEY (symbol, timeframe, open_time)
);
CREATE INDEX IF NOT EXISTS idx_candles_lookup ON candles(symbol, timeframe, open_time);
`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("init sqlite schema failed: %w", err)
	}
	return nil
}

// Close 关闭数据库连接。
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// UpsertCandles 原子写入并在冲突时更新行情。
func (s *SQLiteStore) UpsertCandles(ctx context.Context, candles []Candle) error {
	if len(candles) == 0 {
		return nil
	}

	const stmt = `
INSERT INTO candles (
	symbol, timeframe, open_time, close_time, open, high, low, close, volume, turnover
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(symbol, timeframe, open_time) DO UPDATE SET
	close_time=excluded.close_time,
	open=excluded.open,
	high=excluded.high,
	low=excluded.low,
	close=excluded.close,
	volume=excluded.volume,
	turnover=excluded.turnover;
`

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	prep, err := tx.PrepareContext(ctx, stmt)
	if err != nil {
		return fmt.Errorf("prepare upsert failed: %w", err)
	}
	defer prep.Close()

	for _, c := range candles {
		if _, err := prep.ExecContext(ctx,
			c.Symbol, c.Timeframe, c.OpenTime, c.CloseTime,
			c.Open, c.High, c.Low, c.Close, c.Volume, c.Turnover,
		); err != nil {
			return fmt.Errorf("upsert candle failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert tx failed: %w", err)
	}
	return nil
}

// CountCandles 返回指定 symbol/timeframe 的本地 K 线数量。
func (s *SQLiteStore) CountCandles(ctx context.Context, symbol, timeframe string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM candles WHERE symbol=? AND timeframe=?`, symbol, timeframe)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count candles failed: %w", err)
	}
	return n, nil
}

// LoadCandles 按 open_time 升序读取指定数量的历史 K 线。
func (s *SQLiteStore) LoadCandles(ctx context.Context, symbol, timeframe string, limit int) ([]Candle, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT symbol, timeframe, open_time, close_time, open, high, low, close, volume, turnover
FROM candles
WHERE symbol=? AND timeframe=?
ORDER BY open_time DESC
LIMIT ?`, symbol, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("query candles failed: %w", err)
	}
	defer rows.Close()

	tmp := make([]Candle, 0, limit)
	for rows.Next() {
		var c Candle
		if err := rows.Scan(
			&c.Symbol, &c.Timeframe, &c.OpenTime, &c.CloseTime,
			&c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.Turnover,
		); err != nil {
			return nil, fmt.Errorf("scan candle failed: %w", err)
		}
		tmp = append(tmp, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candles failed: %w", err)
	}

	for i, j := 0, len(tmp)-1; i < j; i, j = i+1, j-1 {
		tmp[i], tmp[j] = tmp[j], tmp[i]
	}
	return tmp, nil
}

// MinOpenTime 返回最早 K 线开盘时间。
func (s *SQLiteStore) MinOpenTime(ctx context.Context, symbol, timeframe string) (int64, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT MIN(open_time) FROM candles WHERE symbol=? AND timeframe=?`, symbol, timeframe)
	var v sql.NullInt64
	if err := row.Scan(&v); err != nil {
		return 0, false, fmt.Errorf("query min open_time failed: %w", err)
	}
	if !v.Valid {
		return 0, false, nil
	}
	return v.Int64, true, nil
}

// MaxOpenTime 返回最新 K 线开盘时间。
func (s *SQLiteStore) MaxOpenTime(ctx context.Context, symbol, timeframe string) (int64, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT MAX(open_time) FROM candles WHERE symbol=? AND timeframe=?`, symbol, timeframe)
	var v sql.NullInt64
	if err := row.Scan(&v); err != nil {
		return 0, false, fmt.Errorf("query max open_time failed: %w", err)
	}
	if !v.Valid {
		return 0, false, nil
	}
	return v.Int64, true, nil
}

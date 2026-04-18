package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"kgod/internal"
)

// BarBroker 定义可由 Bar 驱动撮合的扩展能力。
type BarBroker interface {
	OnBar(bar internal.Bar) error
}

// TickBroker 定义可由 Tick 驱动风险处理的扩展能力。
type TickBroker interface {
	OnTick(tick internal.Tick) error
}

// ProgressSnapshot 表示回测执行进度。
type ProgressSnapshot struct {
	Current     int       `json:"current"`
	Total       int       `json:"total"`
	Percent     float64   `json:"percent"`
	CurrentTime time.Time `json:"currentTime"`
	Status      string    `json:"status"`
}

// BacktestCallbacks 提供回测过程中的可观测回调。
type BacktestCallbacks struct {
	OnBar      func(context.Context, internal.Bar) error
	OnTick     func(context.Context, internal.Tick) error
	OnProgress func(ProgressSnapshot)
}

// BacktestConfig 定义回测引擎配置。
type BacktestConfig struct {
	DataFeed   internal.DataFeed
	Broker     internal.Broker
	Strategy   internal.Strategy
	Symbols    []string
	Timeframes []internal.Timeframe
	From       time.Time
	To         time.Time
	Callbacks  BacktestCallbacks
}

// BacktestEngine 是时间驱动的回测执行器。
type BacktestEngine struct {
	cfg BacktestConfig

	status atomic.Value

	processed atomic.Int64
	total     atomic.Int64

	progressCh chan ProgressSnapshot
	doneCh     chan struct{}

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
}

var _ internal.Engine = (*BacktestEngine)(nil)

// NewBacktestEngine 创建回测引擎实例。
func NewBacktestEngine(cfg BacktestConfig) (*BacktestEngine, error) {
	if cfg.DataFeed == nil {
		return nil, errors.New("data feed is required")
	}
	if cfg.Broker == nil {
		return nil, errors.New("broker is required")
	}
	if cfg.Strategy == nil {
		return nil, errors.New("strategy is required")
	}
	if len(cfg.Symbols) == 0 {
		cfg.Symbols = cfg.Strategy.Symbols()
	}
	if len(cfg.Timeframes) == 0 {
		cfg.Timeframes = cfg.Strategy.Timeframes()
	}
	if len(cfg.Symbols) == 0 || len(cfg.Timeframes) == 0 {
		return nil, errors.New("symbols and timeframes are required")
	}

	e := &BacktestEngine{
		cfg:        cfg,
		progressCh: make(chan ProgressSnapshot, 1024),
		doneCh:     make(chan struct{}),
	}
	e.status.Store(internal.SystemStatusStopped)
	return e, nil
}

// Start 启动回测执行。
func (e *BacktestEngine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return errors.New("backtest engine already started")
	}
	e.started = true
	e.status.Store(internal.SystemStatusStarting)

	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.doneCh = make(chan struct{})

	go func() {
		defer close(e.doneCh)
		if err := e.run(ctx); err != nil {
			e.status.Store(internal.SystemStatusError)
			return
		}
		e.status.Store(internal.SystemStatusStopped)
	}()

	return nil
}

// Stop 停止回测执行。
func (e *BacktestEngine) Stop() error {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return nil
	}
	e.status.Store(internal.SystemStatusStopping)
	cancel := e.cancel
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	<-e.doneCh

	e.mu.Lock()
	e.started = false
	e.cancel = nil
	e.mu.Unlock()
	return nil
}

// GetStatus 返回当前引擎状态。
func (e *BacktestEngine) GetStatus() internal.SystemStatus {
	v := e.status.Load()
	if v == nil {
		return internal.SystemStatusStopped
	}
	return v.(internal.SystemStatus)
}

// Progress 返回当前回测进度快照。
func (e *BacktestEngine) Progress() ProgressSnapshot {
	total := int(e.total.Load())
	current := int(e.processed.Load())
	percent := 0.0
	if total > 0 {
		percent = float64(current) * 100 / float64(total)
	}
	return ProgressSnapshot{
		Current: current,
		Total:   total,
		Percent: percent,
		Status:  string(e.GetStatus()),
	}
}

// ProgressChan 返回进度流，便于 Web 端订阅显示。
func (e *BacktestEngine) ProgressChan() <-chan ProgressSnapshot {
	return e.progressCh
}

func (e *BacktestEngine) run(ctx context.Context) error {
	e.status.Store(internal.SystemStatusBacktesting)

	bars, err := e.loadBars(ctx)
	if err != nil {
		return err
	}
	e.total.Store(int64(len(bars)))
	e.processed.Store(0)

	var queued []internal.OrderRequest

	for i := 0; i < len(bars); i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		bar := bars[i]

		if err := e.flushQueuedOrders(ctx, queued); err != nil {
			return err
		}
		queued = queued[:0]

		if bb, ok := e.cfg.Broker.(BarBroker); ok {
			if err := bb.OnBar(bar); err != nil {
				return fmt.Errorf("broker on bar failed: %w", err)
			}
		}

		if e.cfg.Callbacks.OnBar != nil {
			if err := e.cfg.Callbacks.OnBar(ctx, bar); err != nil {
				return fmt.Errorf("on bar callback failed: %w", err)
			}
		}

		tick := internal.Tick{
			Symbol:    bar.Symbol,
			Price:     bar.Close,
			Bid:       bar.Close,
			Ask:       bar.Close,
			Volume:    bar.Volume,
			Timestamp: bar.EndTime,
		}
		if tb, ok := e.cfg.Broker.(TickBroker); ok {
			if err := tb.OnTick(tick); err != nil {
				return fmt.Errorf("broker on tick failed: %w", err)
			}
		}
		if e.cfg.Callbacks.OnTick != nil {
			if err := e.cfg.Callbacks.OnTick(ctx, tick); err != nil {
				return fmt.Errorf("on tick callback failed: %w", err)
			}
		}

		signals, err := e.cfg.Strategy.OnBar(ctx, bar)
		if err != nil {
			return fmt.Errorf("strategy on bar failed: %w", err)
		}
		for _, sig := range signals {
			orderReq := signalToOrderRequest(sig, bar.EndTime)
			queued = append(queued, orderReq)
		}

		e.processed.Add(1)
		e.pushProgress(bar.EndTime)
	}

	if err := e.flushQueuedOrders(ctx, queued); err != nil {
		return err
	}
	return nil
}

func (e *BacktestEngine) flushQueuedOrders(ctx context.Context, orders []internal.OrderRequest) error {
	for _, req := range orders {
		if _, err := e.cfg.Broker.PlaceOrder(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

func (e *BacktestEngine) loadBars(ctx context.Context) ([]internal.Bar, error) {
	type result struct {
		bars []internal.Bar
		err  error
	}

	ch := make(chan result, len(e.cfg.Symbols)*len(e.cfg.Timeframes))
	var wg sync.WaitGroup

	for _, symbol := range e.cfg.Symbols {
		for _, tf := range e.cfg.Timeframes {
			symbol := symbol
			tf := tf
			if syncer, ok := e.cfg.DataFeed.(interface {
				SyncHistory(symbol string, timeframe string, targetCount int) error
			}); ok {
				cfg, cfgErr := internal.Load()
				if cfgErr == nil {
					if err := syncer.SyncHistory(symbol, string(tf), cfg.BinanceLimit); err != nil {
						return nil, err
					}
				}
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				bars, err := e.cfg.DataFeed.GetHistoricalBars(ctx, symbol, tf, e.cfg.From, e.cfg.To)
				ch <- result{bars: bars, err: err}
			}()
		}
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	merged := make([]internal.Bar, 0, 4096)
	for r := range ch {
		if r.err != nil {
			return nil, r.err
		}
		merged = append(merged, r.bars...)
	}

	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].EndTime.Equal(merged[j].EndTime) {
			if merged[i].Symbol == merged[j].Symbol {
				return merged[i].Timeframe < merged[j].Timeframe
			}
			return merged[i].Symbol < merged[j].Symbol
		}
		return merged[i].EndTime.Before(merged[j].EndTime)
	})
	return merged, nil
}

func (e *BacktestEngine) pushProgress(currentTime time.Time) {
	snap := e.Progress()
	snap.CurrentTime = currentTime

	select {
	case e.progressCh <- snap:
	default:
	}

	if e.cfg.Callbacks.OnProgress != nil {
		e.cfg.Callbacks.OnProgress(snap)
	}
}

func signalToOrderRequest(sig internal.Signal, ts time.Time) internal.OrderRequest {
	side := sig.Side
	if side == "" {
		switch sig.Action {
		case internal.SignalActionBuy:
			side = internal.SideBuy
		case internal.SignalActionSell, internal.SignalActionClose:
			side = internal.SideSell
		default:
			side = internal.SideBuy
		}
	}

	return internal.OrderRequest{
		ClientOrderID: fmt.Sprintf("sig-%d", ts.UnixNano()),
		StrategyID:    sig.StrategyID,
		Symbol:        sig.Symbol,
		Side:          side,
		Type:          sig.OrderType,
		Quantity:      sig.Quantity,
		LimitPrice:    sig.LimitPrice,
		StopPrice:     sig.StopPrice,
		TimeInForce:   internal.TimeInForceGTC,
		CreatedAt:     ts,
		Timeframe:     sig.Timeframe,
	}
}

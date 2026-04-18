package main

import (
	"context"
	"log"
	"time"

	"kgod/internal"
	"kgod/internal/broker"
	"kgod/internal/datafeed"
	"kgod/internal/database"
	"kgod/internal/engine"
	"kgod/internal/reporter"
	"kgod/internal/strategy"
)

type historicalFeed struct {
	bars []internal.Bar
}

func (h *historicalFeed) SubscribeTicks(_ context.Context, _ []string) (<-chan internal.Tick, <-chan error) {
	tickCh := make(chan internal.Tick)
	errCh := make(chan error)
	close(tickCh)
	close(errCh)
	return tickCh, errCh
}

func (h *historicalFeed) SubscribeBars(_ context.Context, _ []string, _ internal.Timeframe) (<-chan internal.Bar, <-chan error) {
	barCh := make(chan internal.Bar)
	errCh := make(chan error)
	close(barCh)
	close(errCh)
	return barCh, errCh
}

func (h *historicalFeed) GetHistoricalBars(_ context.Context, symbol string, timeframe internal.Timeframe, from, to time.Time) ([]internal.Bar, error) {
	out := make([]internal.Bar, 0, len(h.bars))
	for _, b := range h.bars {
		if b.Symbol != symbol || b.Timeframe != timeframe {
			continue
		}
		if !from.IsZero() && b.EndTime.Before(from) {
			continue
		}
		if !to.IsZero() && b.EndTime.After(to) {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

func main() {
	cfg, err := internal.Load()
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	symbol := cfg.Symbol
	tf := internal.Timeframe(cfg.Timeframe)
	if tf == "" {
		tf = internal.Timeframe1h
	}

	store, err := database.NewSQLiteStore("kgod.sqlite")
	if err != nil {
		log.Fatalf("init sqlite store failed: %v", err)
	}
	defer store.Close()

	feed := datafeed.NewBinanceDataFeedWithStore(store)
	if feed == nil {
		log.Fatalf("create binance datafeed failed")
	}

	if err := feed.SyncHistory(symbol, string(tf), cfg.BinanceLimit); err != nil {
		log.Fatalf("sync history failed: %v", err)
	}

	bars, err := feed.GetHistoricalBars(context.Background(), symbol, tf, time.Time{}, time.Time{})
	if err != nil {
		log.Fatalf("load historical bars failed: %v", err)
	}
	if len(bars) == 0 {
		log.Fatalf("historical bars returned empty data")
	}

	from := bars[0].StartTime
	to := bars[len(bars)-1].EndTime
	pb := broker.NewPaperBroker(broker.PaperBrokerConfig{
		InitialBalance:        cfg.InitialBalance,
		DefaultLeverage:       cfg.DefaultLeverage,
		DefaultMarginType:     internal.MarginTypeCrossed,
		MaintenanceMarginRate: 0.005,
		MakerFeeRate:          0.0002,
		TakerFeeRate:          0.0005,
		SlippageBps:           2,
		MaxFillRatio:          0.5,
		Debug:                 true,
	})
	st := strategy.NewSMCStrategy([]string{symbol}, []internal.Timeframe{tf})

	eng, err := engine.NewBacktestEngine(engine.BacktestConfig{
		DataFeed:   feed,
		Broker:     pb,
		Strategy:   st,
		Symbols:    []string{symbol},
		Timeframes: []internal.Timeframe{tf},
		From:       from,
		To:         to,
		Callbacks: engine.BacktestCallbacks{
			OnProgress: func(p engine.ProgressSnapshot) {
				log.Printf("progress: %d/%d (%.2f%%) at %s status=%s", p.Current, p.Total, p.Percent, p.CurrentTime.Format(time.RFC3339), p.Status)
			},
		},
	})
	if err != nil {
		log.Fatalf("new backtest engine failed: %v", err)
	}

	if err := eng.Start(); err != nil {
		log.Fatalf("engine start failed: %v", err)
	}

	for {
		status := eng.GetStatus()
		if status == internal.SystemStatusStopped || status == internal.SystemStatusError {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := eng.Stop(); err != nil {
		log.Fatalf("engine stop failed: %v", err)
	}

	report := reporter.BuildPerformanceReport(pb.GetInitialBalance(), pb.GetEquityCurve(), pb.GetClosedTrades())

	if _, _, err := reporter.PersistPerformanceReport(report, "reports"); err != nil {
		log.Fatalf("persist tearsheet failed: %v", err)
	}
}

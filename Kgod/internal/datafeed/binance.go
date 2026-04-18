package datafeed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"kgod/internal"
	"kgod/internal/database"
)

// BinanceDataFeed 提供 Binance U 本位合约历史行情数据接入。
type BinanceDataFeed struct {
	client  *futures.Client
	store   *database.SQLiteStore
	initErr error
}

var _ internal.DataFeed = (*BinanceDataFeed)(nil)

// NewBinanceDataFeed 创建 Futures 数据源。
func NewBinanceDataFeed() *BinanceDataFeed {
	return NewBinanceDataFeedWithStore(nil)
}

// NewBinanceDataFeedWithStore 创建带本地持久化能力的数据源。
func NewBinanceDataFeedWithStore(store *database.SQLiteStore) *BinanceDataFeed {
	cfg, err := internal.Load()
	if err != nil {
		return &BinanceDataFeed{initErr: fmt.Errorf("load config failed: %w", err)}
	}

	client := futures.NewClient("", "")

	if cfg.FuturesBaseURL != "" {
		baseURL, err := url.Parse(cfg.FuturesBaseURL)
		if err != nil {
			return &BinanceDataFeed{initErr: fmt.Errorf("parse FUTURES_BASE_URL failed: %w", err)}
		}
		if baseURL.Scheme == "" || baseURL.Host == "" {
			return &BinanceDataFeed{initErr: fmt.Errorf("invalid FUTURES_BASE_URL: %q", cfg.FuturesBaseURL)}
		}
		client.BaseURL = cfg.FuturesBaseURL
	}

	if cfg.HTTPProxy != "" {
		proxyURL, err := url.Parse(cfg.HTTPProxy)
		if err != nil {
			return &BinanceDataFeed{initErr: fmt.Errorf("parse HTTP_PROXY failed: %w", err)}
		}
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		httpClient := &http.Client{Transport: transport, Timeout: 60 * time.Second}
		client.HTTPClient = httpClient
	}

	return &BinanceDataFeed{client: client, store: store}
}

// SyncHistory 通过分页请求增量同步历史 K 线至本地数据库。
func (b *BinanceDataFeed) SyncHistory(symbol string, timeframe string, targetCount int) error {
	if b.initErr != nil {
		return b.initErr
	}
	if b.store == nil {
		return errors.New("sqlite store is required for sync")
	}
	if symbol == "" {
		return errors.New("symbol is required")
	}

	cfg, err := internal.Load()
	if err != nil {
		return fmt.Errorf("load config failed: %w", err)
	}
	if timeframe == "" {
		timeframe = cfg.Timeframe
	}
	if timeframe == "" {
		return errors.New("timeframe is required")
	}
	if targetCount <= 0 {
		targetCount = cfg.BinanceLimit
	}
	if targetCount <= 0 {
		targetCount = 1500
	}

	pageLimit := cfg.BinanceLimit
	if pageLimit <= 0 {
		pageLimit = 1500
	}
	if pageLimit > 1500 {
		pageLimit = 1500
	}

	ctx := context.Background()
	count, err := b.store.CountCandles(ctx, symbol, timeframe)
	if err != nil {
		return fmt.Errorf("count local candles failed: %w", err)
	}

	startTime := int64(0)
	if maxOpenTime, ok, err := b.store.MaxOpenTime(ctx, symbol, timeframe); err != nil {
		return fmt.Errorf("query max local open_time failed: %w", err)
	} else if ok {
		startTime = maxOpenTime + 1
	}

	for count < targetCount {
		svc := b.client.NewKlinesService().Symbol(symbol).Interval(timeframe).Limit(pageLimit)
		if startTime > 0 {
			svc = svc.StartTime(startTime)
		}

		klines, err := svc.Do(ctx)
		if err != nil {
			return fmt.Errorf("sync futures klines failed: %w", err)
		}
		if len(klines) == 0 {
			break
		}

		candles := make([]database.Candle, 0, len(klines))
		lastOpenTime := startTime
		for i, k := range klines {
			if k == nil {
				return fmt.Errorf("kline at index %d is nil", i)
			}
			candle, err := toCandle(symbol, timeframe, k)
			if err != nil {
				return err
			}
			candles = append(candles, candle)
			if candle.OpenTime > lastOpenTime {
				lastOpenTime = candle.OpenTime
			}
		}

		if err := b.store.UpsertCandles(ctx, candles); err != nil {
			return fmt.Errorf("upsert candles failed: %w", err)
		}

		count, err = b.store.CountCandles(ctx, symbol, timeframe)
		if err != nil {
			return fmt.Errorf("count local candles failed: %w", err)
		}
		if lastOpenTime <= startTime {
			break
		}
		startTime = lastOpenTime + 1
		if len(klines) < pageLimit {
			break
		}
	}

	return nil
}

// FetchHistoricalBars 拉取历史 K 线并输出 Tick 序列。
func (b *BinanceDataFeed) FetchHistoricalBars(symbol string, interval string, limit int) ([]*internal.Tick, error) {
	if b.initErr != nil {
		return nil, b.initErr
	}
	if symbol == "" {
		return nil, errors.New("symbol is required")
	}
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}

	cfg, err := internal.Load()
	if err != nil {
		return nil, fmt.Errorf("load config failed: %w", err)
	}
	if interval == "" {
		interval = cfg.Timeframe
	}
	if interval == "" {
		return nil, errors.New("interval is required")
	}
	if limit > cfg.BinanceLimit && cfg.BinanceLimit > 0 {
		limit = cfg.BinanceLimit
	}

	if b.store != nil {
		if err := b.SyncHistory(symbol, interval, limit); err != nil {
			return nil, err
		}
		candles, err := b.store.LoadCandles(context.Background(), symbol, interval, limit)
		if err != nil {
			return nil, fmt.Errorf("load candles from db failed: %w", err)
		}
		out := make([]*internal.Tick, 0, len(candles))
		for _, c := range candles {
			out = append(out, &internal.Tick{
				Symbol:    c.Symbol,
				Price:     c.Close,
				Bid:       c.Low,
				Ask:       c.High,
				Volume:    c.Volume,
				Timestamp: time.UnixMilli(c.CloseTime).UTC(),
			})
		}
		return out, nil
	}

	klines, err := b.client.NewKlinesService().Symbol(symbol).Interval(interval).Limit(limit).Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("fetch futures klines failed: %w", err)
	}

	out := make([]*internal.Tick, 0, len(klines))
	for i, k := range klines {
		if k == nil {
			return nil, fmt.Errorf("kline at index %d is nil", i)
		}
		high, err := parseFloat(k.High, "high")
		if err != nil {
			return nil, fmt.Errorf("parse high failed at index %d: %w", i, err)
		}
		low, err := parseFloat(k.Low, "low")
		if err != nil {
			return nil, fmt.Errorf("parse low failed at index %d: %w", i, err)
		}
		closePrice, err := parseFloat(k.Close, "close")
		if err != nil {
			return nil, fmt.Errorf("parse close failed at index %d: %w", i, err)
		}
		volume, err := parseFloat(k.Volume, "volume")
		if err != nil {
			return nil, fmt.Errorf("parse volume failed at index %d: %w", i, err)
		}
		out = append(out, &internal.Tick{
			Symbol:    symbol,
			Price:     closePrice,
			Bid:       low,
			Ask:       high,
			Volume:    volume,
			Timestamp: time.UnixMilli(k.CloseTime).UTC(),
		})
	}
	return out, nil
}

// GetHistoricalBars 拉取指定时间区间的历史 Bar。
func (b *BinanceDataFeed) GetHistoricalBars(ctx context.Context, symbol string, timeframe internal.Timeframe, from, to time.Time) ([]internal.Bar, error) {
	if b.initErr != nil {
		return nil, b.initErr
	}
	if symbol == "" {
		return nil, errors.New("symbol is required")
	}
	if timeframe == "" {
		return nil, errors.New("timeframe is required")
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, errors.New("from must be before to")
	}

	cfg, err := internal.Load()
	if err != nil {
		return nil, fmt.Errorf("load config failed: %w", err)
	}

	interval := string(timeframe)
	if cfg.Timeframe != "" {
		interval = cfg.Timeframe
	}

	if b.store != nil {
		if err := b.SyncHistory(symbol, interval, cfg.BinanceLimit); err != nil {
			return nil, err
		}
		candles, err := b.store.LoadCandles(ctx, symbol, interval, cfg.BinanceLimit)
		if err != nil {
			return nil, fmt.Errorf("load candles from db failed: %w", err)
		}
		bars := make([]internal.Bar, 0, len(candles))
		for _, c := range candles {
			bar := internal.Bar{
				Symbol:    c.Symbol,
				Timeframe: timeframe,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
				Turnover:  c.Turnover,
				StartTime: time.UnixMilli(c.OpenTime).UTC(),
				EndTime:   time.UnixMilli(c.CloseTime).UTC(),
			}
			if !from.IsZero() && bar.EndTime.Before(from) {
				continue
			}
			if !to.IsZero() && bar.EndTime.After(to) {
				continue
			}
			bars = append(bars, bar)
		}
		return bars, nil
	}

	svc := b.client.NewKlinesService().Symbol(symbol).Interval(interval).Limit(cfg.BinanceLimit)
	if !from.IsZero() {
		svc = svc.StartTime(from.UnixMilli())
	}
	if !to.IsZero() {
		svc = svc.EndTime(to.UnixMilli())
	}

	klines, err := svc.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get futures historical bars failed: %w", err)
	}

	bars := make([]internal.Bar, 0, len(klines))
	for i, k := range klines {
		if k == nil {
			return nil, fmt.Errorf("kline at index %d is nil", i)
		}
		openPrice, err := parseFloat(k.Open, "open")
		if err != nil {
			return nil, fmt.Errorf("parse open failed at index %d: %w", i, err)
		}
		highPrice, err := parseFloat(k.High, "high")
		if err != nil {
			return nil, fmt.Errorf("parse high failed at index %d: %w", i, err)
		}
		lowPrice, err := parseFloat(k.Low, "low")
		if err != nil {
			return nil, fmt.Errorf("parse low failed at index %d: %w", i, err)
		}
		closePrice, err := parseFloat(k.Close, "close")
		if err != nil {
			return nil, fmt.Errorf("parse close failed at index %d: %w", i, err)
		}
		volume, err := parseFloat(k.Volume, "volume")
		if err != nil {
			return nil, fmt.Errorf("parse volume failed at index %d: %w", i, err)
		}
		turnover, err := parseFloat(k.QuoteAssetVolume, "quoteAssetVolume")
		if err != nil {
			turnover = 0
		}
		bars = append(bars, internal.Bar{
			Symbol:    symbol,
			Timeframe: timeframe,
			Open:      openPrice,
			High:      highPrice,
			Low:       lowPrice,
			Close:     closePrice,
			Volume:    volume,
			Turnover:  turnover,
			StartTime: time.UnixMilli(k.OpenTime).UTC(),
			EndTime:   time.UnixMilli(k.CloseTime).UTC(),
		})
	}
	return bars, nil
}

// SubscribeTicks 当前迭代未实现实时流。
func (b *BinanceDataFeed) SubscribeTicks(_ context.Context, _ []string) (<-chan internal.Tick, <-chan error) {
	tickCh := make(chan internal.Tick)
	errCh := make(chan error, 1)
	errCh <- errors.New("SubscribeTicks is not implemented")
	close(tickCh)
	close(errCh)
	return tickCh, errCh
}

// SubscribeBars 当前迭代未实现实时流。
func (b *BinanceDataFeed) SubscribeBars(_ context.Context, _ []string, _ internal.Timeframe) (<-chan internal.Bar, <-chan error) {
	barCh := make(chan internal.Bar)
	errCh := make(chan error, 1)
	errCh <- errors.New("SubscribeBars is not implemented")
	close(barCh)
	close(errCh)
	return barCh, errCh
}

func toCandle(symbol, timeframe string, k *futures.Kline) (database.Candle, error) {
	openPrice, err := parseFloat(k.Open, "open")
	if err != nil {
		return database.Candle{}, fmt.Errorf("parse open failed: %w", err)
	}
	highPrice, err := parseFloat(k.High, "high")
	if err != nil {
		return database.Candle{}, fmt.Errorf("parse high failed: %w", err)
	}
	lowPrice, err := parseFloat(k.Low, "low")
	if err != nil {
		return database.Candle{}, fmt.Errorf("parse low failed: %w", err)
	}
	closePrice, err := parseFloat(k.Close, "close")
	if err != nil {
		return database.Candle{}, fmt.Errorf("parse close failed: %w", err)
	}
	volume, err := parseFloat(k.Volume, "volume")
	if err != nil {
		return database.Candle{}, fmt.Errorf("parse volume failed: %w", err)
	}
	turnover, err := parseFloat(k.QuoteAssetVolume, "quoteAssetVolume")
	if err != nil {
		turnover = 0
	}
	return database.Candle{
		Symbol:    symbol,
		Timeframe: timeframe,
		OpenTime:  k.OpenTime,
		CloseTime: k.CloseTime,
		Open:      openPrice,
		High:      highPrice,
		Low:       lowPrice,
		Close:     closePrice,
		Volume:    volume,
		Turnover:  turnover,
	}, nil
}

func parseFloat(raw, field string) (float64, error) {
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s failed: %w", field, err)
	}
	return v, nil
}

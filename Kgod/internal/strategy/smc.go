package strategy

import (
	"context"
	"log"
	"sync"
	"time"

	"kgod/internal"
)

type fvgKind uint8

const (
	fvgBullish fvgKind = 1
	fvgBearish fvgKind = 2
)

type fvgZone struct {
	Kind      fvgKind
	Symbol    string
	Timeframe internal.Timeframe
	Low       float64
	High      float64
	CreatedAt time.Time
	Filled    bool
	Touched   bool
}

// SMCStrategy 基于 SMC 的 FVG 缺口回踩模型。
type SMCStrategy struct {
	symbols    []string
	timeframes []internal.Timeframe
	strategyID string

	mu       sync.Mutex
	bars     map[string][]internal.Bar
	zones    map[string][]*fvgZone
	lastTick map[string]internal.Tick
}

var _ internal.Strategy = (*SMCStrategy)(nil)

// NewSMCStrategy 创建 SMC 策略实例。
func NewSMCStrategy(symbols []string, timeframes []internal.Timeframe) *SMCStrategy {
	if len(symbols) == 0 {
		symbols = []string{"ETHUSDT"}
	}
	if len(timeframes) == 0 {
		timeframes = []internal.Timeframe{internal.Timeframe1h}
	}
	return &SMCStrategy{
		symbols:    symbols,
		timeframes: timeframes,
		strategyID: "smc-fvg-v1",
		bars:       make(map[string][]internal.Bar, len(symbols)*len(timeframes)),
		zones:      make(map[string][]*fvgZone, len(symbols)*len(timeframes)),
		lastTick:   make(map[string]internal.Tick, len(symbols)),
	}
}

func (s *SMCStrategy) Name() string {
	return s.strategyID
}

func (s *SMCStrategy) Symbols() []string {
	out := make([]string, len(s.symbols))
	copy(out, s.symbols)
	return out
}

func (s *SMCStrategy) Timeframes() []internal.Timeframe {
	out := make([]internal.Timeframe, len(s.timeframes))
	copy(out, s.timeframes)
	return out
}

func (s *SMCStrategy) OnTick(_ context.Context, tick internal.Tick) ([]internal.Signal, error) {
	s.mu.Lock()
	s.lastTick[tick.Symbol] = tick
	s.mu.Unlock()
	return nil, nil
}

func (s *SMCStrategy) OnBar(_ context.Context, bar internal.Bar) ([]internal.Signal, error) {
	key := seriesKey(bar.Symbol, bar.Timeframe)

	s.mu.Lock()
	defer s.mu.Unlock()

	seq := append(s.bars[key], bar)
	s.bars[key] = seq

	if len(seq) >= 3 {
		if z := detectNewFVG(seq, bar.Symbol, bar.Timeframe); z != nil {
			s.zones[key] = append(s.zones[key], z)
			log.Printf("[SMC] FVG detected symbol=%s tf=%s kind=%s at=%s range=[%.6f, %.6f]",
				z.Symbol, z.Timeframe, zoneKindString(z.Kind), z.CreatedAt.Format(time.RFC3339), z.Low, z.High)
		}
	}

	price := bar.Close
	if tk, ok := s.lastTick[bar.Symbol]; ok && !tk.Timestamp.IsZero() {
		price = tk.Price
	}

	zones := s.zones[key]
	for i := len(zones) - 1; i >= 0; i-- {
		z := zones[i]
		if z.Filled {
			continue
		}

		if bar.Low <= z.Low && bar.High >= z.High {
			z.Filled = true
			continue
		}

		if z.Touched {
			continue
		}

		if !inRange(price, z.Low, z.High) {
			continue
		}

		z.Touched = true
		if z.Kind == fvgBullish {
			return []internal.Signal{newSignal(s.strategyID, bar, internal.SignalActionBuy, internal.SideBuy)}, nil
		}
		return []internal.Signal{newSignal(s.strategyID, bar, internal.SignalActionSell, internal.SideSell)}, nil
	}

	return nil, nil
}

func detectNewFVG(seq []internal.Bar, symbol string, tf internal.Timeframe) *fvgZone {
	i := len(seq) - 1
	left := seq[i-2]
	right := seq[i]

	if left.High < right.Low {
		return &fvgZone{
			Kind:      fvgBullish,
			Symbol:    symbol,
			Timeframe: tf,
			Low:       left.High,
			High:      right.Low,
			CreatedAt: right.EndTime,
		}
	}

	if left.Low > right.High {
		return &fvgZone{
			Kind:      fvgBearish,
			Symbol:    symbol,
			Timeframe: tf,
			Low:       right.High,
			High:      left.Low,
			CreatedAt: right.EndTime,
		}
	}

	return nil
}

func newSignal(strategyID string, bar internal.Bar, action internal.SignalAction, side internal.Side) internal.Signal {
	return internal.Signal{
		StrategyID: strategyID,
		Symbol:     bar.Symbol,
		Timeframe:  bar.Timeframe,
		Action:     action,
		OrderType:  internal.OrderTypeMarket,
		Side:       side,
		Quantity:   1,
		Confidence: 1,
		Timestamp:  bar.EndTime,
	}
}

func inRange(price, low, high float64) bool {
	return price >= low && price <= high
}

func zoneKindString(kind fvgKind) string {
	if kind == fvgBullish {
		return "bullish"
	}
	return "bearish"
}

func seriesKey(symbol string, tf internal.Timeframe) string {
	return symbol + "|" + string(tf)
}

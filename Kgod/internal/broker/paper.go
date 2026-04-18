package broker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"kgod/internal"
)

// PaperBrokerConfig 定义 U 本位模拟撮合器参数。
type PaperBrokerConfig struct {
	InitialBalance         float64
	DefaultLeverage        int
	DefaultMarginType      internal.MarginType
	MaintenanceMarginRate  float64
	MakerFeeRate           float64
	TakerFeeRate           float64
	SlippageBps            float64
	MaxFillRatio           float64
	Debug                  bool
	Logger                 *log.Logger
}

type contractPosition struct {
	Symbol                string
	PositionSide          internal.PositionSide
	Quantity              float64
	AvgPrice              float64
	MarginUsed            float64
	Leverage              int
	MarginType            internal.MarginType
	MaintenanceMarginRate float64
	LiquidationPrice      float64
	RealizedPnL           float64
	UnrealizedPnL         float64
}

type tradeState struct {
	EntryTime   time.Time
	RealizedPnL float64
}

// EquityPoint 表示账户权益时间序列点。
type EquityPoint struct {
	Timestamp time.Time
	Equity    float64
}

// ClosedTrade 表示一次完整开平仓后的已闭合交易。
type ClosedTrade struct {
	Symbol       string
	PositionSide internal.PositionSide
	EntryTime    time.Time
	ExitTime     time.Time
	RealizedPnL  float64
}

// PaperBroker 是 U 本位永续模拟撮合器。
type PaperBroker struct {
	mu sync.RWMutex

	ordersByID map[string]*internal.Order
	activeByID map[string]struct{}

	fills      []internal.Fill
	equity     []EquityPoint
	closed     []ClosedTrade
	tradeState map[string]tradeState
	lastPrice  map[string]float64
	positions  map[string]*contractPosition
	balance    float64

	initialBalance float64

	defaultLeverage       int
	defaultMarginType     internal.MarginType
	maintenanceMarginRate float64
	makerFeeRate          float64
	takerFeeRate          float64
	slippageBps           float64
	maxFillRatio          float64
	debug                 bool
	logger                *log.Logger

	orderSeq atomic.Uint64
	fillSeq  atomic.Uint64
}

var _ internal.Broker = (*PaperBroker)(nil)

// TickBroker 扩展能力：可由逐 Tick 驱动风控。
type TickBroker interface {
	OnTick(tick internal.Tick) error
}

var _ TickBroker = (*PaperBroker)(nil)

// BarBroker 扩展能力：可由 Bar 驱动撮合。
type BarBroker interface {
	OnBar(bar internal.Bar) error
}

var _ BarBroker = (*PaperBroker)(nil)

// NewPaperBroker 创建合约模拟撮合器。
func NewPaperBroker(cfg PaperBrokerConfig) *PaperBroker {
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(os.Stdout, "[paper-broker] ", log.LstdFlags|log.Lmicroseconds)
	}
	lev := cfg.DefaultLeverage
	if lev <= 0 {
		lev = 1
	}
	marginType := cfg.DefaultMarginType
	if marginType == "" {
		marginType = internal.MarginTypeCrossed
	}
	maxFill := cfg.MaxFillRatio
	if maxFill <= 0 || maxFill > 1 {
		maxFill = 1
	}

	p := &PaperBroker{
		ordersByID:             make(map[string]*internal.Order, 256),
		activeByID:             make(map[string]struct{}, 256),
		fills:                  make([]internal.Fill, 0, 1024),
		equity:                 make([]EquityPoint, 0, 1024),
		closed:                 make([]ClosedTrade, 0, 512),
		tradeState:             make(map[string]tradeState, 64),
		lastPrice:              make(map[string]float64, 64),
		positions:              make(map[string]*contractPosition, 64),
		balance:                cfg.InitialBalance,
		initialBalance:         cfg.InitialBalance,
		defaultLeverage:        lev,
		defaultMarginType:      marginType,
		maintenanceMarginRate:  cfg.MaintenanceMarginRate,
		makerFeeRate:           cfg.MakerFeeRate,
		takerFeeRate:           cfg.TakerFeeRate,
		slippageBps:            cfg.SlippageBps,
		maxFillRatio:           maxFill,
		debug:                  cfg.Debug,
		logger:                 logger,
	}
	p.equity = append(p.equity, EquityPoint{Timestamp: time.Now().UTC(), Equity: cfg.InitialBalance})
	return p
}

// PlaceOrder 提交订单并执行初始保证金校验。
func (p *PaperBroker) PlaceOrder(_ context.Context, req internal.OrderRequest) (internal.Order, error) {
	if req.Symbol == "" {
		return internal.Order{}, errors.New("symbol is required")
	}
	if req.Quantity <= 0 {
		return internal.Order{}, errors.New("quantity must be positive")
	}
	if req.Type == internal.OrderTypeLimit && req.LimitPrice <= 0 {
		return internal.Order{}, errors.New("limit order requires limitPrice")
	}
	if req.Type == internal.OrderTypeStop && req.StopPrice <= 0 {
		return internal.Order{}, errors.New("stop order requires stopPrice")
	}

	now := time.Now().UTC()
	if req.CreatedAt.IsZero() {
		req.CreatedAt = now
	}
	if req.TimeInForce == "" {
		req.TimeInForce = internal.TimeInForceGTC
	}
	if req.Leverage <= 0 {
		req.Leverage = p.defaultLeverage
	}
	if req.MarginType == "" {
		req.MarginType = p.defaultMarginType
	}
	if req.PositionSide == "" {
		req.PositionSide = sideToPositionSide(req.Side)
	}

	orderID := fmt.Sprintf("ord-%d", p.orderSeq.Add(1))
	order := internal.Order{
		ID:            orderID,
		ClientOrderID: req.ClientOrderID,
		StrategyID:    req.StrategyID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Status:        internal.OrderStatusPending,
		Quantity:      req.Quantity,
		FilledQty:     0,
		LimitPrice:    req.LimitPrice,
		StopPrice:     req.StopPrice,
		TimeInForce:   req.TimeInForce,
		ReduceOnly:    req.ReduceOnly,
		Leverage:      req.Leverage,
		MarginType:    req.MarginType,
		PositionSide:  req.PositionSide,
		CreatedAt:     req.CreatedAt,
		UpdatedAt:     now,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	price := p.lastPrice[order.Symbol]
	if price <= 0 {
		price = fallbackOrderPrice(order)
	}
	if err := p.ensureInitialMarginLocked(order, price); err != nil {
		order.Status = internal.OrderStatusRejected
		p.ordersByID[order.ID] = &order
		return order, err
	}

	p.ordersByID[order.ID] = &order
	p.activeByID[order.ID] = struct{}{}

	if p.debug {
		p.logger.Printf("submit futures order id=%s symbol=%s side=%s posSide=%s type=%s qty=%.6f lev=%d",
			order.ID, order.Symbol, order.Side, order.PositionSide, order.Type, order.Quantity, order.Leverage)
	}

	if order.Type == internal.OrderTypeMarket && price > 0 {
		_, _ = p.fillOrderLocked(&order, price, req.Quantity, now, false)
	}

	return *p.ordersByID[order.ID], nil
}

// CancelOrder 撤销活动订单。
func (p *PaperBroker) CancelOrder(_ context.Context, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ord, ok := p.ordersByID[orderID]
	if !ok {
		return errors.New("order not found")
	}
	if ord.Status == internal.OrderStatusFilled || ord.Status == internal.OrderStatusCanceled {
		return nil
	}
	ord.Status = internal.OrderStatusCanceled
	ord.UpdatedAt = time.Now().UTC()
	delete(p.activeByID, orderID)
	return nil
}

// GetActiveOrders 获取活动订单。
func (p *PaperBroker) GetActiveOrders(_ context.Context, symbol string) ([]internal.Order, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]internal.Order, 0, len(p.activeByID))
	for id := range p.activeByID {
		ord := p.ordersByID[id]
		if symbol != "" && ord.Symbol != symbol {
			continue
		}
		out = append(out, *ord)
	}
	return out, nil
}

// OnTick 按逐 Tick 推进价格与强平引擎。
func (p *PaperBroker) OnTick(tick internal.Tick) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if tick.Price <= 0 {
		return nil
	}
	p.lastPrice[tick.Symbol] = tick.Price
	p.updatePnLLocked(tick.Symbol, tick.Price)
	return p.runLiquidationCheckLocked(tick.Symbol, tick.Price, tick.Timestamp)
}

// OnBar 按 Bar 推进撮合与强平。
func (p *PaperBroker) OnBar(bar internal.Bar) error {
	if err := p.OnTick(internal.Tick{
		Symbol:    bar.Symbol,
		Price:     bar.Close,
		Bid:       bar.Low,
		Ask:       bar.High,
		Volume:    bar.Volume,
		Timestamp: bar.EndTime,
	}); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := bar.EndTime
	for id := range p.activeByID {
		ord := p.ordersByID[id]
		if ord.Symbol != bar.Symbol {
			continue
		}
		if ord.Status != internal.OrderStatusPending && ord.Status != internal.OrderStatusPartiallyFilled {
			continue
		}

		trigger, fill, maker := shouldFillOrder(*ord, bar)
		if !fill {
			continue
		}
		remaining := ord.Quantity - ord.FilledQty
		qty := computeFillQty(remaining, bar.Volume, p.maxFillRatio)
		if qty <= 0 {
			continue
		}
		if _, err := p.fillOrderLocked(ord, trigger, qty, now, maker); err != nil {
			return err
		}
	}
	return nil
}

// GetFills 返回成交记录。
func (p *PaperBroker) GetFills() []internal.Fill {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]internal.Fill, len(p.fills))
	copy(out, p.fills)
	return out
}

// GetInitialBalance 返回初始资金。
func (p *PaperBroker) GetInitialBalance() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.initialBalance
}

// GetEquityCurve 返回账户权益曲线。
func (p *PaperBroker) GetEquityCurve() []EquityPoint {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]EquityPoint, len(p.equity))
	copy(out, p.equity)
	return out
}

// GetClosedTrades 返回已平仓交易。
func (p *PaperBroker) GetClosedTrades() []ClosedTrade {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ClosedTrade, len(p.closed))
	copy(out, p.closed)
	return out
}

// GetPosition 返回聚合持仓快照。
func (p *PaperBroker) GetPosition(symbol string) (internal.Position, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	longPos := p.positions[positionKey(symbol, internal.PositionSideLong)]
	shortPos := p.positions[positionKey(symbol, internal.PositionSideShort)]
	if longPos == nil && shortPos == nil {
		return internal.Position{}, false
	}

	mark := p.lastPrice[symbol]
	if longPos != nil {
		return positionSnapshot(*longPos, mark), true
	}
	return positionSnapshot(*shortPos, mark), true
}

func (p *PaperBroker) ensureInitialMarginLocked(order internal.Order, mark float64) error {
	if order.ReduceOnly {
		return nil
	}
	if mark <= 0 {
		return nil
	}
	notional := mark * order.Quantity
	requiredMargin := notional / float64(order.Leverage)
	requiredFee := notional * p.takerFeeRate
	if p.availableBalanceLocked() < requiredMargin+requiredFee {
		return fmt.Errorf("insufficient margin: required=%.6f available=%.6f", requiredMargin+requiredFee, p.availableBalanceLocked())
	}
	return nil
}

func (p *PaperBroker) fillOrderLocked(order *internal.Order, triggerPrice, fillQty float64, ts time.Time, maker bool) (internal.Fill, error) {
	if fillQty <= 0 {
		return internal.Fill{}, errors.New("fill quantity must be positive")
	}
	remaining := order.Quantity - order.FilledQty
	if fillQty > remaining {
		fillQty = remaining
	}

	execPrice := applySlippage(triggerPrice, order.Side, p.slippageBps)
	notional := execPrice * fillQty
	feeRate := p.takerFeeRate
	if maker {
		feeRate = p.makerFeeRate
	}
	fee := notional * feeRate

	realized := p.applyPositionFillLocked(order, fillQty, execPrice, fee)
	p.balance += realized

	prevFilled := order.FilledQty
	order.FilledQty += fillQty
	if order.FilledQty < order.Quantity {
		order.Status = internal.OrderStatusPartiallyFilled
	} else {
		order.Status = internal.OrderStatusFilled
		delete(p.activeByID, order.ID)
	}
	order.AvgFillPrice = weightedAvg(order.AvgFillPrice, prevFilled, execPrice, fillQty)
	order.UpdatedAt = ts

	fill := internal.Fill{
		OrderID:       order.ID,
		Symbol:        order.Symbol,
		Side:          order.Side,
		Price:         execPrice,
		Quantity:      fillQty,
		Fee:           fee,
		RealizedPnL:   realized,
		ExecutedAt:    ts,
		TransactionID: fmt.Sprintf("fill-%d", p.fillSeq.Add(1)),
	}
	p.fills = append(p.fills, fill)
	p.recordTradeStateLocked(fill, order.PositionSide)

	if p.debug {
		p.logger.Printf("fill futures order id=%s status=%s qty=%.6f/%.6f exec=%.6f fee=%.6f realized=%.6f",
			order.ID, order.Status, order.FilledQty, order.Quantity, execPrice, fee, realized)
	}

	p.updatePnLLocked(order.Symbol, p.lastPrice[order.Symbol])
	return fill, nil
}

func (p *PaperBroker) applyPositionFillLocked(order *internal.Order, qty, price, fee float64) float64 {
	key := positionKey(order.Symbol, order.PositionSide)
	pos := p.positions[key]
	if pos == nil {
		pos = &contractPosition{
			Symbol:                order.Symbol,
			PositionSide:          order.PositionSide,
			Leverage:              order.Leverage,
			MarginType:            order.MarginType,
			MaintenanceMarginRate: p.maintenanceMarginRate,
		}
		p.positions[key] = pos
	}

	signedDelta := qty
	if order.PositionSide == internal.PositionSideShort {
		signedDelta = -qty
	}
	if order.Side == internal.SideSell && order.PositionSide == internal.PositionSideLong {
		signedDelta = -qty
	}
	if order.Side == internal.SideBuy && order.PositionSide == internal.PositionSideShort {
		signedDelta = qty
	}

	current := pos.Quantity
	realized := 0.0

	if current == 0 || current*signedDelta > 0 {
		newQty := current + signedDelta
		pos.AvgPrice = weightedAvg(pos.AvgPrice, math.Abs(current), price, math.Abs(signedDelta))
		pos.Quantity = newQty
		pos.MarginUsed += (price * qty) / float64(maxInt(1, pos.Leverage))
		realized -= fee
		pos.RealizedPnL += realized
		return realized
	}

	closing := math.Min(math.Abs(current), math.Abs(signedDelta))
	realized += (price - pos.AvgPrice) * closing * sign(current)
	leftQty := current + signedDelta
	if math.Abs(leftQty) < 1e-12 {
		pos.Quantity = 0
		pos.AvgPrice = 0
		pos.MarginUsed = 0
	} else {
		pos.Quantity = leftQty
		if current*leftQty < 0 {
			pos.AvgPrice = price
		}
		pos.MarginUsed = (math.Abs(pos.Quantity) * pos.AvgPrice) / float64(maxInt(1, pos.Leverage))
	}
	realized -= fee
	pos.RealizedPnL += realized
	return realized
}

func (p *PaperBroker) updatePnLLocked(symbol string, mark float64) {
	for _, side := range []internal.PositionSide{internal.PositionSideLong, internal.PositionSideShort} {
		pos := p.positions[positionKey(symbol, side)]
		if pos == nil || pos.Quantity == 0 {
			continue
		}
		pos.UnrealizedPnL = (mark - pos.AvgPrice) * pos.Quantity
		if pos.MarginUsed > 0 {
			if pos.Quantity > 0 {
				pos.LiquidationPrice = pos.AvgPrice - (pos.MarginUsed*(1-pos.MaintenanceMarginRate))/math.Abs(pos.Quantity)
			} else {
				pos.LiquidationPrice = pos.AvgPrice + (pos.MarginUsed*(1-pos.MaintenanceMarginRate))/math.Abs(pos.Quantity)
			}
		}
	}
	p.appendEquityPointLocked(time.Now().UTC())
}

func (p *PaperBroker) appendEquityPointLocked(ts time.Time) {
	equity := p.balance
	for _, pos := range p.positions {
		equity += pos.UnrealizedPnL
	}
	if len(p.equity) > 0 {
		last := p.equity[len(p.equity)-1]
		if !ts.After(last.Timestamp) {
			ts = last.Timestamp.Add(time.Nanosecond)
		}
	}
	p.equity = append(p.equity, EquityPoint{Timestamp: ts, Equity: equity})
}

func (p *PaperBroker) recordTradeStateLocked(fill internal.Fill, positionSide internal.PositionSide) {
	key := positionKey(fill.Symbol, positionSide)
	state := p.tradeState[key]
	pos := p.positions[key]
	if state.EntryTime.IsZero() {
		state.EntryTime = fill.ExecutedAt
	}
	state.RealizedPnL += fill.RealizedPnL
	if pos == nil || math.Abs(pos.Quantity) < 1e-12 {
		p.closed = append(p.closed, ClosedTrade{
			Symbol:       fill.Symbol,
			PositionSide: positionSide,
			EntryTime:    state.EntryTime,
			ExitTime:     fill.ExecutedAt,
			RealizedPnL:  state.RealizedPnL,
		})
		delete(p.tradeState, key)
		return
	}
	p.tradeState[key] = state
}

func (p *PaperBroker) runLiquidationCheckLocked(symbol string, mark float64, ts time.Time) error {
	for _, side := range []internal.PositionSide{internal.PositionSideLong, internal.PositionSideShort} {
		pos := p.positions[positionKey(symbol, side)]
		if pos == nil || pos.Quantity == 0 {
			continue
		}
		notional := math.Abs(pos.Quantity) * mark
		maintenance := notional * pos.MaintenanceMarginRate
		equity := p.balance + pos.UnrealizedPnL
		if equity >= maintenance {
			continue
		}

		if p.debug {
			p.logger.Printf("LIQUIDATION TRIGGERED symbol=%s side=%s mark=%.6f equity=%.6f maintenance=%.6f",
				symbol, side, mark, equity, maintenance)
		}

		qty := math.Abs(pos.Quantity)
		liqOrder := internal.Order{
			ID:           fmt.Sprintf("liq-%d", p.orderSeq.Add(1)),
			Symbol:       symbol,
			Side:         liquidationSide(side),
			Type:         internal.OrderTypeMarket,
			Status:       internal.OrderStatusPending,
			Quantity:     qty,
			PositionSide: side,
			Leverage:     pos.Leverage,
			MarginType:   pos.MarginType,
			CreatedAt:    ts,
			UpdatedAt:    ts,
		}
		if _, err := p.fillOrderLocked(&liqOrder, mark, qty, ts, false); err != nil {
			return err
		}
		liqOrder.Status = internal.OrderStatusFilled
		p.ordersByID[liqOrder.ID] = &liqOrder
	}
	return nil
}

func (p *PaperBroker) availableBalanceLocked() float64 {
	used := 0.0
	for _, pos := range p.positions {
		used += pos.MarginUsed
	}
	return p.balance - used
}

func shouldFillOrder(order internal.Order, bar internal.Bar) (float64, bool, bool) {
	switch order.Type {
	case internal.OrderTypeMarket:
		return bar.Close, true, false
	case internal.OrderTypeLimit:
		if order.Side == internal.SideBuy && bar.Low <= order.LimitPrice {
			return order.LimitPrice, true, true
		}
		if order.Side == internal.SideSell && bar.High >= order.LimitPrice {
			return order.LimitPrice, true, true
		}
	case internal.OrderTypeStop:
		if order.Side == internal.SideBuy && bar.High >= order.StopPrice {
			return order.StopPrice, true, false
		}
		if order.Side == internal.SideSell && bar.Low <= order.StopPrice {
			return order.StopPrice, true, false
		}
	}
	return 0, false, false
}

func computeFillQty(remaining, barVolume, maxFillRatio float64) float64 {
	if remaining <= 0 {
		return 0
	}
	if barVolume <= 0 {
		return remaining
	}
	cap := barVolume * maxFillRatio
	if cap <= 0 || cap > remaining {
		return remaining
	}
	return cap
}

func applySlippage(price float64, side internal.Side, slippageBps float64) float64 {
	if slippageBps == 0 {
		return price
	}
	factor := slippageBps / 10000.0
	if side == internal.SideBuy {
		return price * (1 + factor)
	}
	return price * (1 - factor)
}

func fallbackOrderPrice(order internal.Order) float64 {
	if order.Type == internal.OrderTypeLimit && order.LimitPrice > 0 {
		return order.LimitPrice
	}
	if order.Type == internal.OrderTypeStop && order.StopPrice > 0 {
		return order.StopPrice
	}
	return 0
}

func sideToPositionSide(side internal.Side) internal.PositionSide {
	if side == internal.SideSell {
		return internal.PositionSideShort
	}
	return internal.PositionSideLong
}

func liquidationSide(positionSide internal.PositionSide) internal.Side {
	if positionSide == internal.PositionSideShort {
		return internal.SideBuy
	}
	return internal.SideSell
}

func positionSnapshot(pos contractPosition, mark float64) internal.Position {
	return internal.Position{
		Symbol:                 pos.Symbol,
		NetQuantity:            pos.Quantity,
		AvgPrice:               pos.AvgPrice,
		MarkPrice:              mark,
		Leverage:               pos.Leverage,
		MarginType:             pos.MarginType,
		PositionSide:           pos.PositionSide,
		MarginUsed:             pos.MarginUsed,
		MaintenanceMarginRate:  pos.MaintenanceMarginRate,
		LiquidationPrice:       pos.LiquidationPrice,
		UnrealizedPnL:          pos.UnrealizedPnL,
		RealizedPnL:            pos.RealizedPnL,
		UpdatedAt:              time.Now().UTC(),
	}
}

func positionKey(symbol string, side internal.PositionSide) string {
	return symbol + "|" + string(side)
}

func weightedAvg(aPrice, aQty, bPrice, bQty float64) float64 {
	total := aQty + bQty
	if total == 0 {
		return 0
	}
	return (aPrice*aQty + bPrice*bQty) / total
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

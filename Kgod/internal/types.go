package internal

import (
	"context"
	"time"
)

// SystemStatus 表示交易引擎的全局运行状态。
type SystemStatus string

const (
	// SystemStatusStopped 表示引擎已停止。
	SystemStatusStopped SystemStatus = "stopped"
	// SystemStatusStarting 表示引擎正在启动。
	SystemStatusStarting SystemStatus = "starting"
	// SystemStatusBacktesting 表示引擎处于回测模式。
	SystemStatusBacktesting SystemStatus = "backtesting"
	// SystemStatusLive 表示引擎处于实盘模式。
	SystemStatusLive SystemStatus = "live"
	// SystemStatusStopping 表示引擎正在停止。
	SystemStatusStopping SystemStatus = "stopping"
	// SystemStatusError 表示引擎处于错误状态。
	SystemStatusError SystemStatus = "error"
)

// OrderStatus 表示订单生命周期状态。
type OrderStatus string

const (
	// OrderStatusPending 表示订单已创建，等待成交。
	OrderStatusPending OrderStatus = "pending"
	// OrderStatusPartiallyFilled 表示订单部分成交。
	OrderStatusPartiallyFilled OrderStatus = "partiallyFilled"
	// OrderStatusFilled 表示订单全部成交。
	OrderStatusFilled OrderStatus = "filled"
	// OrderStatusCanceled 表示订单已撤销。
	OrderStatusCanceled OrderStatus = "canceled"
	// OrderStatusRejected 表示订单被拒绝。
	OrderStatusRejected OrderStatus = "rejected"
	// OrderStatusExpired 表示订单已过期。
	OrderStatusExpired OrderStatus = "expired"
)

// OrderType 表示下单类型。
type OrderType string

const (
	// OrderTypeMarket 市价单。
	OrderTypeMarket OrderType = "market"
	// OrderTypeLimit 限价单。
	OrderTypeLimit OrderType = "limit"
	// OrderTypeStop 止损单。
	OrderTypeStop OrderType = "stop"
)

// Side 表示交易方向。
type Side string

const (
	// SideBuy 买入方向。
	SideBuy Side = "buy"
	// SideSell 卖出方向。
	SideSell Side = "sell"
)

// MarginType 表示保证金模式。
type MarginType string

const (
	// MarginTypeCrossed 表示全仓模式。
	MarginTypeCrossed MarginType = "CROSSED"
	// MarginTypeIsolated 表示逐仓模式。
	MarginTypeIsolated MarginType = "ISOLATED"
)

// PositionSide 表示双向持仓方向。
type PositionSide string

const (
	// PositionSideLong 表示多头仓位。
	PositionSideLong PositionSide = "LONG"
	// PositionSideShort 表示空头仓位。
	PositionSideShort PositionSide = "SHORT"
)

// TimeInForce 表示订单有效期策略。
type TimeInForce string

const (
	// TimeInForceGTC Good Till Canceled。
	TimeInForceGTC TimeInForce = "gtc"
	// TimeInForceIOC Immediate Or Cancel。
	TimeInForceIOC TimeInForce = "ioc"
	// TimeInForceFOK Fill Or Kill。
	TimeInForceFOK TimeInForce = "fok"
)

// SignalAction 表示策略输出的交易动作。
type SignalAction string

const (
	// SignalActionBuy 表示做多/加多。
	SignalActionBuy SignalAction = "buy"
	// SignalActionSell 表示做空/减多。
	SignalActionSell SignalAction = "sell"
	// SignalActionClose 表示平仓。
	SignalActionClose SignalAction = "close"
	// SignalActionHold 表示观望。
	SignalActionHold SignalAction = "hold"
)

// Timeframe 表示K线周期标识。
type Timeframe string

const (
	Timeframe1m Timeframe = "1m"
	Timeframe5m Timeframe = "5m"
	Timeframe15m Timeframe = "15m"
	Timeframe1h Timeframe = "1h"
	Timeframe4h Timeframe = "4h"
	Timeframe1d Timeframe = "1d"
)

// Tick 表示最小粒度的市场快照数据。
type Tick struct {
	Symbol    string    `json:"symbol"`
	Price     float64   `json:"price"`
	Bid       float64   `json:"bid"`
	Ask       float64   `json:"ask"`
	Volume    float64   `json:"volume"`
	Timestamp time.Time `json:"timestamp"`
}

// Bar 表示时间聚合后的行情数据。
type Bar struct {
	Symbol    string    `json:"symbol"`
	Timeframe Timeframe `json:"timeframe"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	Turnover  float64   `json:"turnover"`
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
}

// OrderRequest 表示统一下单请求。
type OrderRequest struct {
	ClientOrderID string      `json:"clientOrderId"`
	StrategyID    string      `json:"strategyId"`
	Symbol        string      `json:"symbol"`
	Side          Side        `json:"side"`
	Type          OrderType   `json:"type"`
	Quantity      float64     `json:"quantity"`
	LimitPrice    float64     `json:"limitPrice,omitempty"`
	StopPrice     float64     `json:"stopPrice,omitempty"`
	TimeInForce   TimeInForce  `json:"timeInForce"`
	ReduceOnly    bool         `json:"reduceOnly"`
	Leverage      int          `json:"leverage"`
	MarginType    MarginType   `json:"marginType"`
	PositionSide  PositionSide `json:"positionSide"`
	Timeframe     Timeframe    `json:"timeframe,omitempty"`
	CreatedAt     time.Time    `json:"createdAt"`
}

// Order 表示路由后的订单实体。
type Order struct {
	ID            string      `json:"id"`
	ClientOrderID string      `json:"clientOrderId"`
	StrategyID    string      `json:"strategyId"`
	Symbol        string      `json:"symbol"`
	Side          Side        `json:"side"`
	Type          OrderType   `json:"type"`
	Status        OrderStatus `json:"status"`
	Quantity      float64     `json:"quantity"`
	FilledQty     float64     `json:"filledQty"`
	LimitPrice    float64     `json:"limitPrice,omitempty"`
	StopPrice     float64     `json:"stopPrice,omitempty"`
	AvgFillPrice  float64     `json:"avgFillPrice,omitempty"`
	TimeInForce   TimeInForce  `json:"timeInForce"`
	ReduceOnly    bool         `json:"reduceOnly"`
	Leverage      int          `json:"leverage"`
	MarginType    MarginType   `json:"marginType"`
	PositionSide  PositionSide `json:"positionSide"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

// Fill 表示成交回报事件。
type Fill struct {
	OrderID       string    `json:"orderId"`
	Symbol        string    `json:"symbol"`
	Side          Side      `json:"side"`
	Price         float64   `json:"price"`
	Quantity      float64   `json:"quantity"`
	Fee           float64   `json:"fee"`
	RealizedPnL   float64   `json:"realizedPnl"`
	ExecutedAt    time.Time `json:"executedAt"`
	TransactionID string    `json:"transactionId"`
}

// Position 表示单品种持仓状态；NetQuantity > 0 为多头，< 0 为空头。
type Position struct {
	Symbol        string    `json:"symbol"`
	NetQuantity   float64   `json:"netQuantity"`
	AvgPrice      float64   `json:"avgPrice"`
	MarkPrice              float64      `json:"markPrice"`
	Leverage               int          `json:"leverage"`
	MarginType             MarginType   `json:"marginType"`
	PositionSide           PositionSide `json:"positionSide"`
	MarginUsed             float64      `json:"marginUsed"`
	MaintenanceMarginRate  float64      `json:"maintenanceMarginRate"`
	LiquidationPrice       float64      `json:"liquidationPrice"`
	UnrealizedPnL          float64      `json:"unrealizedPnl"`
	RealizedPnL            float64      `json:"realizedPnl"`
	UpdatedAt              time.Time    `json:"updatedAt"`
}

// Account 表示账户资金总览。
type Account struct {
	AccountID     string    `json:"accountId"`
	Currency      string    `json:"currency"`
	Balance       float64   `json:"balance"`
	Equity        float64   `json:"equity"`
	Available     float64   `json:"available"`
	MarginUsed    float64   `json:"marginUsed"`
	MarginRatio   float64   `json:"marginRatio"`
	Leverage      float64   `json:"leverage"`
	UnrealizedPnL float64   `json:"unrealizedPnl"`
	RealizedPnL   float64   `json:"realizedPnl"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Signal 表示策略输出的标准交易信号。
type Signal struct {
	StrategyID string            `json:"strategyId"`
	Symbol     string            `json:"symbol"`
	Timeframe  Timeframe         `json:"timeframe"`
	Action     SignalAction      `json:"action"`
	OrderType  OrderType         `json:"orderType"`
	Side       Side              `json:"side"`
	Quantity   float64           `json:"quantity"`
	LimitPrice float64           `json:"limitPrice,omitempty"`
	StopPrice  float64           `json:"stopPrice,omitempty"`
	Confidence float64           `json:"confidence"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

// Engine 定义系统核心控制器契约，供 Web API 直接调用。
type Engine interface {
	Start() error
	Stop() error
	GetStatus() SystemStatus
}

// DataFeed 定义行情数据源契约，支持实时订阅与历史拉取。
type DataFeed interface {
	SubscribeTicks(ctx context.Context, symbols []string) (<-chan Tick, <-chan error)
	SubscribeBars(ctx context.Context, symbols []string, timeframe Timeframe) (<-chan Bar, <-chan error)
	GetHistoricalBars(ctx context.Context, symbol string, timeframe Timeframe, from, to time.Time) ([]Bar, error)
}

// Broker 定义券商路由契约，支持下单、撤单和活跃订单查询。
type Broker interface {
	PlaceOrder(ctx context.Context, req OrderRequest) (Order, error)
	CancelOrder(ctx context.Context, orderID string) error
	GetActiveOrders(ctx context.Context, symbol string) ([]Order, error)
}

// Portfolio 定义资金与仓位管理契约，需覆盖杠杆与空头逻辑。
type Portfolio interface {
	ApplyFill(fill Fill) error
	GetPosition(symbol string) (Position, bool)
	ListPositions() []Position
	GetAccount() Account
	CalculateAvailableBalance(leverage float64, includeShort bool) float64
}

// RiskManager 定义交易前风控拦截契约。
type RiskManager interface {
	ValidateOrder(ctx context.Context, req OrderRequest, account Account, positions []Position) error
}

// Strategy 定义策略基类契约，支持多品种与多时间框架输入。
type Strategy interface {
	Name() string
	Symbols() []string
	Timeframes() []Timeframe
	OnTick(ctx context.Context, tick Tick) ([]Signal, error)
	OnBar(ctx context.Context, bar Bar) ([]Signal, error)
}

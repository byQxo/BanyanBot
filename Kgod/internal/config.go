package internal

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

type Config struct {
	ExchangeEnv            string
	Symbol                 string
	HTTPProxy              string
	FuturesBaseURL         string
	Timeframe              string
	BinanceLimit           int
	InitialBalance         float64
	DefaultLeverage        int
	MarginMode             string
	PositionMode           string
	MaxDrawdownStop        float64
	RiskPerTradePercent    float64
	FVGMinSize             float64
	OrderBlockLookback     int
	LiquiditySweepTolerance float64
	BOSConfirmationCandles int
	TakeProfitRR           float64
	StopLossBuffer         float64
	LLMProvider            string
	LLMAPIKey              string
	LLMModelID             string
	LLMConfidenceThreshold float64
}

var (
	globalConfig Config
	loadErr      error
	loadOnce     sync.Once
)

func Load() (Config, error) {
	loadOnce.Do(func() {
		_ = godotenv.Load()

		cfg := Config{}
		cfg.ExchangeEnv = envString("EXCHANGE_ENV", "testnet")
		cfg.Symbol = envString("SYMBOL", "ETHUSDT")
		cfg.HTTPProxy = envString("HTTP_PROXY", "")
		cfg.FuturesBaseURL = envString("FUTURES_BASE_URL", "https://fapi.binance.com")
		cfg.Timeframe = envString("TIMEFRAME", "1h")
		cfg.BinanceLimit = envInt("BINANCE_LIMIT", 1500)
		cfg.InitialBalance = envFloat64("INITIAL_BALANCE", 10000.0)
		cfg.DefaultLeverage = envInt("DEFAULT_LEVERAGE", 10)
		cfg.MarginMode = envMarginMode("MARGIN_MODE", "ISOLATED")
		cfg.PositionMode = envPositionMode("POSITION_MODE", "ONEWAY")
		cfg.MaxDrawdownStop = envFloat64("MAX_DRAWDOWN_STOP", 0.20)
		cfg.RiskPerTradePercent = envFloat64("RISK_PER_TRADE_PERCENT", 0.02)
		cfg.FVGMinSize = envFloat64("FVG_MIN_SIZE", 0.0)
		cfg.OrderBlockLookback = envInt("ORDER_BLOCK_LOOKBACK", 50)
		cfg.LiquiditySweepTolerance = envFloat64("LIQUIDITY_SWEEP_TOLERANCE", 0.1)
		cfg.BOSConfirmationCandles = envInt("BOS_CONFIRMATION_CANDLES", 1)
		cfg.TakeProfitRR = envFloat64("TAKE_PROFIT_RR", 2.0)
		cfg.StopLossBuffer = envFloat64("STOP_LOSS_BUFFER", 0.001)
		cfg.LLMProvider = envString("LLM_PROVIDER", "anthropic")
		cfg.LLMAPIKey = envString("LLM_API_KEY", "")
		cfg.LLMModelID = envString("LLM_MODEL_ID", "claude-3-5-sonnet-20241022")
		cfg.LLMConfidenceThreshold = envFloat64("LLM_CONFIDENCE_THRESHOLD", 0.80)
		globalConfig = cfg
	})

	if loadErr != nil {
		return Config{}, loadErr
	}
	return globalConfig, nil
}

func Current() Config {
	return globalConfig
}

func envString(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envFloat64(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return v
}

func envMarginMode(key, fallback string) string {
	v := strings.ToUpper(envString(key, fallback))
	if v != "ISOLATED" && v != "CROSS" {
		return fallback
	}
	return v
}

func envPositionMode(key, fallback string) string {
	v := strings.ToUpper(envString(key, fallback))
	if v != "ONEWAY" && v != "HEDGE" {
		return fallback
	}
	return v
}

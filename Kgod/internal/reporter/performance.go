package reporter

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kgod/internal/broker"
)

// PerformanceReport 表示回测绩效评估结果。
type PerformanceReport struct {
	InitialCapital    float64 `json:"initialCapital"`
	FinalEquity       float64 `json:"finalEquity"`
	NetProfit         float64 `json:"netProfit"`
	ReturnPct         float64 `json:"returnPct"`
	ClosedTrades      int     `json:"closedTrades"`
	WinningTrades     int     `json:"winningTrades"`
	LosingTrades      int     `json:"losingTrades"`
	WinRate           float64 `json:"winRate"`
	AverageWin        float64 `json:"averageWin"`
	AverageLoss       float64 `json:"averageLoss"`
	ProfitFactor      float64 `json:"profitFactor"`
	MaxDrawdownAmount float64 `json:"maxDrawdownAmount"`
	MaxDrawdownPct    float64 `json:"maxDrawdownPct"`
}

// BuildPerformanceReport 基于资金曲线与已平仓交易生成绩效报告。
func BuildPerformanceReport(initialCapital float64, equityCurve []broker.EquityPoint, closedTrades []broker.ClosedTrade) PerformanceReport {
	report := PerformanceReport{
		InitialCapital: initialCapital,
		FinalEquity:    initialCapital,
	}

	if len(equityCurve) > 0 {
		report.FinalEquity = equityCurve[len(equityCurve)-1].Equity
	}
	report.NetProfit = report.FinalEquity - report.InitialCapital
	if report.InitialCapital != 0 {
		report.ReturnPct = report.NetProfit / report.InitialCapital * 100
	}

	report.ClosedTrades = len(closedTrades)
	var grossProfit float64
	var grossLossAbs float64

	for _, t := range closedTrades {
		switch {
		case t.RealizedPnL > 0:
			report.WinningTrades++
			grossProfit += t.RealizedPnL
		case t.RealizedPnL < 0:
			report.LosingTrades++
			grossLossAbs += math.Abs(t.RealizedPnL)
		}
	}

	if report.ClosedTrades > 0 {
		report.WinRate = float64(report.WinningTrades) / float64(report.ClosedTrades) * 100
	}
	if report.WinningTrades > 0 {
		report.AverageWin = grossProfit / float64(report.WinningTrades)
	}
	if report.LosingTrades > 0 {
		report.AverageLoss = grossLossAbs / float64(report.LosingTrades)
	}
	if grossLossAbs > 0 {
		report.ProfitFactor = grossProfit / grossLossAbs
	}

	report.MaxDrawdownAmount, report.MaxDrawdownPct = computeMaxDrawdown(equityCurve)
	return report
}

// PersistPerformanceReport 将报告写入 JSON 与 Markdown 文件。
func PersistPerformanceReport(report PerformanceReport, reportsDir string) (string, string, error) {
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create reports dir failed: %w", err)
	}

	loc := time.FixedZone("CST", 8*3600)
	now := time.Now().In(loc)
	timeTag := now.Format("20060102_1504")
	baseName := fmt.Sprintf("tearsheet_%s", timeTag)
	jsonPath := filepath.Join(reportsDir, baseName+".json")
	mdPath := filepath.Join(reportsDir, baseName+".md")

	jsonBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal report json failed: %w", err)
	}
	jsonFile, err := os.OpenFile(jsonPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("open report json file failed: %w", err)
	}
	defer jsonFile.Close()
	if _, err := jsonFile.Write(append(jsonBytes, '\n')); err != nil {
		return "", "", fmt.Errorf("write report json failed: %w", err)
	}

	markdown := renderMarkdown(report, now)
	mdFile, err := os.OpenFile(mdPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", "", fmt.Errorf("open report markdown file failed: %w", err)
	}
	defer mdFile.Close()
	if _, err := mdFile.Write([]byte(markdown)); err != nil {
		return "", "", fmt.Errorf("write report markdown failed: %w", err)
	}

	return jsonPath, mdPath, nil
}

func computeMaxDrawdown(curve []broker.EquityPoint) (float64, float64) {
	if len(curve) == 0 {
		return 0, 0
	}

	highWaterMark := curve[0].Equity
	maxDrawdownAmount := 0.0
	maxDrawdownPct := 0.0

	for _, point := range curve {
		if point.Equity > highWaterMark {
			highWaterMark = point.Equity
		}
		drawdownAmount := highWaterMark - point.Equity
		if drawdownAmount > maxDrawdownAmount {
			maxDrawdownAmount = drawdownAmount
		}
		if highWaterMark > 0 {
			drawdownPct := drawdownAmount / highWaterMark * 100
			if drawdownPct > maxDrawdownPct {
				maxDrawdownPct = drawdownPct
			}
		}
	}

	return maxDrawdownAmount, maxDrawdownPct
}

func renderMarkdown(report PerformanceReport, ts time.Time) string {
	t := ts.Format(time.RFC3339)
	var b strings.Builder
	b.WriteString("# Backtest Tear Sheet\n\n")
	b.WriteString("- GeneratedAt: " + t + "\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("| --- | ---: |\n")
	b.WriteString(fmt.Sprintf("| Initial Capital | %.4f |\n", report.InitialCapital))
	b.WriteString(fmt.Sprintf("| Final Equity | %.4f |\n", report.FinalEquity))
	b.WriteString(fmt.Sprintf("| Net Profit | %.4f |\n", report.NetProfit))
	b.WriteString(fmt.Sprintf("| Return (%%) | %.2f%% |\n", report.ReturnPct))
	b.WriteString(fmt.Sprintf("| Closed Trades | %d |\n", report.ClosedTrades))
	b.WriteString(fmt.Sprintf("| Winning Trades | %d |\n", report.WinningTrades))
	b.WriteString(fmt.Sprintf("| Losing Trades | %d |\n", report.LosingTrades))
	b.WriteString(fmt.Sprintf("| Win Rate (%%) | %.2f%% |\n", report.WinRate))
	b.WriteString(fmt.Sprintf("| Average Win | %.4f |\n", report.AverageWin))
	b.WriteString(fmt.Sprintf("| Average Loss | %.4f |\n", report.AverageLoss))
	b.WriteString(fmt.Sprintf("| Profit Factor | %.4f |\n", report.ProfitFactor))
	b.WriteString(fmt.Sprintf("| Max Drawdown Amount | %.4f |\n", report.MaxDrawdownAmount))
	b.WriteString(fmt.Sprintf("| Max Drawdown (%%) | %.2f%% |\n", report.MaxDrawdownPct))
	return b.String()
}

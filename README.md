# 🌲 BanyanBot (Kgod Engine)

![Golang](https://img.shields.io/badge/Language-Go-00ADD8?style=for-the-badge&logo=go)
![Architecture](https://img.shields.io/badge/Architecture-SMC-F78166?style=for-the-badge)
![Status](https://img.shields.io/badge/Status-Alpha-critical?style=for-the-badge)

> **"Decoupling assets from human error via sovereign code."**
> 专为加密资产市场锻造的工业级量化交易引擎，适合虚拟货币与底层高频管线构建。

## 🛡️ Core Architecture | 核心架构

BanyanBot 并非简单的脚本，而是一套具备极强横向扩展能力的量化基建。其核心模块包含：

* **SMC 决策矩阵 (Smart Money Engine)**
  * 精准捕捉 FVG (Fair Value Gap) 价格缺口。
  * 流动性清扫 (Liquidity Sweep) 容错与订单块 (Order Block) 追踪。
* **物理级数据馈送 (Datafeed Plumbing)**
  * 废弃脆弱的单点 API 请求，采用多级游标分页 (Cursor Pagination) 机制。
  * 搭载 SQLite 嵌入式数据库，实现毫秒级本地回测与断点续传。
* **隔离风控系统 (Isolated Risk Management)**
  * 22 项全量解耦的动态配置矩阵 (`.env` 驱动)。
  * 严格执行单笔交易风险敞口 (`RISK_PER_TRADE`) 与全局最大回撤熔断 (`MAX_DRAWDOWN`)。

----------------------------------------------------------------------------------

## 🗺️ Engineering Roadmap | 改进目标


### Phase 1: Infrastructure & Plumbing (基建) - `[COMPLETED]`
- [  ] 搭建基础撮合引擎与 Binance Futures 鉴权路由
- [ ] 注入 22 项工业级全局配置矩阵（彻底消灭硬编码）
- [ ] 部署基础 SMC/FVG 识别算法与回测沙盘
- [ ] 构建 SQLite 数据持久化层与历史 K 线泵流

### Phase 2: Live Execution & Telemetry (实盘与遥测) - `[IN PROGRESS]`
- [ ] **实盘交易总线接入 (Live API Order Execution)**
- [ ] 实盘与回测环境的一键物理切换 (`testnet` -> `live`)
- [ ] WSS 全双工实时行情监听 (Websocket Data Pump)
- [ ] 飞书/Telegram 实时战报与异常告警推送

### Phase 3: Cognitive Integration (未来目标) - `[PLANNED]`
- [ ] 预留算力通道，接入大模型 (Claude/Anthropic) 决策接驳层
- [ ] 基于 AI 置信度的动态仓位调节系统
- [ ] Web 级可视化监控面板 (Banyan Dashboard)

---

## ⚠️ 声明

本项目目前处于架构成型期。金融市场需敬畏，代码的任何一次越界都可能导致不可逆的资产回撤。
**永远不要将你无法承受损失的密钥注入本系统的实盘环境变量中。**

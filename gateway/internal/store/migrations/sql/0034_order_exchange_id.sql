-- 0034: xt_orders 增加 exchange_order_id（交易所侧订单号）
-- 日期: 2026-10-01
--
-- 背景: 币安适配器早已实现 QueryOrderStatus/GetOrderTrades（binance_reconcile.go），
-- 但 ① 返回类型是 adapter 包的私有类型，不满足 reconcile 包的窄接口
-- （OrderStatusQuerier/OrderTradesQuerier），类型断言永远失败
-- ② 交易所 orderId（数字）从未持久化——reconcile 拿本地 ord-* ID 去查币安
-- 必然查不到。2026-10-01 生产实测: 实盘市价单被币安接受(orderId 671122224734)
-- 后本地 OMS 永停 NEW，成交恢复每轮刷"未实现订单查询接口"。
--
-- 本迁移只加列。写入路径（OMS 捕获 ACK 的 orderId）与接口签名修复随代码落地。

ALTER TABLE xt_orders ADD COLUMN exchange_order_id TEXT DEFAULT ''

-- 0037: 信号执行器 K 线收盘止损（对标 CryptoRobotics Candle Close SL）
-- 语义：盘中跌破止损线不平仓，只有 K 线"收盘价"跌破才触发（防插针/影线假跌破）。
-- candle_close_interval 为判定用 K 线周期（默认 1m）。

ALTER TABLE xt_signal_executions ADD COLUMN candle_close_sl INTEGER NOT NULL DEFAULT 0;
ALTER TABLE xt_signal_executions ADD COLUMN candle_close_interval TEXT NOT NULL DEFAULT '1m';

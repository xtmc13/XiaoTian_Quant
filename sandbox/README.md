# XiaoTianQuant Sandbox

Python ML/指标沙箱服务——量化交易的机器学习与指标计算后端。

## 服务列表

| 服务 | 端口 | 说明 |
|------|------|------|
| ML Server | 8001 | LightGBM/XGBoost 模型训练与预测 |
| CCXT Bridge | 8002 | CCXT 统一交易所接口 HTTP 封装 |
| TensorBoard | 6006 | 强化学习训练可视化 |
| Indicators | 9000 | 技术指标计算 |

## 目录结构

```
sandbox/
├── main.py                  # 指标服务入口
├── ccxt_bridge/
│   └── main.py              # CCXT 交易所桥接 HTTP 服务
├── indicators/
│   └── zero_lag_trend.py    # 自定义指标实现
├── ml_server/
│   ├── main.py              # ML 服务入口
│   ├── feature_engine.py    # 特征工程
│   ├── label_creator.py     # 标签生成
│   ├── data_kitchen.py      # 数据预处理
│   ├── models/
│   │   ├── base_model.py    # 模型基类
│   │   ├── lightgbm_model.py # LightGBM
│   │   ├── xgboost_model.py  # XGBoost
│   │   └── rl_env.py        # 强化学习环境
│   ├── rl_trainer.py        # RL 训练器
│   ├── rl_worker.py         # RL 分布式训练 worker
│   └── tensorboard_server.py # TensorBoard 服务
├── requirements.txt
└── Dockerfile
```

## 快速开始

```bash
# 安装依赖
pip install -r requirements.txt

# 启动 ML Server
cd ml_server
python main.py

# 启动 CCXT Bridge
cd ccxt_bridge
python main.py

# 启动 TensorBoard (RL 训练时)
tensorboard --logdir logs/ --port 6006
```

## 服务接口

### ML Server (`:8001`)

```
POST /train        # 训练模型
POST /predict      # 预测
GET  /models       # 列出模型
```

### CCXT Bridge (`:8002`)

```
POST /ticker       # 获取行情
POST /orderbook    # 获取订单簿
POST /trades       # 获取成交
POST /balance      # 获取余额
```

## Docker 构建

```bash
docker build -t xiaotian-sandbox .
docker run -p 8001:8001 xiaotian-sandbox
```

## 依赖

Python 3.12 + pandas + numpy + scikit-learn + lightgbm + xgboost + ccxt + ray[rllib]

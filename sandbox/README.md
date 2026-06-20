# XiaoTianQuant Sandbox

Python 训练与指标沙箱——量化交易的机器学习训练与指标计算工具集。

> **注意：** Python 不再作为常驻服务运行。已从 Go+Rust+TS+Python 四栈架构回退到 Go+Rust+TS 三栈架构。
> Python 仅保留为**命令行训练/导出工具**。

## 工具列表

| 工具 | 入口 | 说明 |
|------|------|------|
| Sandbox CLI | `python sandbox/main.py` | 指标代码执行与静态分析 |
| Training CLI | `python sandbox/train.py` | LightGBM/XGBoost 模型训练与导出 |

## 目录结构

```
sandbox/
├── main.py                  # Sandbox CLI 入口（指标执行与分析）
├── train.py                 # Training CLI 入口（模型训练与导出）
├── ccxt_bridge/             # CCXT 交易所桥接（保留代码，不再常驻）
│   └── main.py
├── indicators/
│   └── zero_lag_trend.py    # 自定义指标实现
├── ml_server/               # ML 核心模块（被 train.py 引用）
│   ├── main.py              # ML Training CLI
│   ├── feature_engine.py    # 特征工程
│   ├── label_creator.py     # 标签生成
│   ├── data_kitchen.py      # 数据预处理
│   ├── models/
│   │   ├── base_model.py    # 模型基类
│   │   ├── lightgbm_model.py # LightGBM
│   │   └── xgboost_model.py  # XGBoost
│   ├── rl_trainer.py        # RL 训练器
│   └── tensorboard_server.py # TensorBoard 服务
├── requirements.txt
└── Dockerfile
```

## 快速开始

```bash
# 安装依赖
pip install -r requirements.txt

# 查看帮助
python sandbox/main.py --help
python sandbox/train.py --help

# 执行指标代码
python sandbox/main.py execute --code 'output = {"plots": [], "signals": []}'

# 分析指标代码质量
python sandbox/main.py analyze --code 'output = {"plots": [], "signals": []}'

# 训练模型并导出 JSON 树
python sandbox/train.py train \
  --data ./data/btc_1h.json \
  --model-type lightgbm \
  --output ./models/btc_model \
  --horizon 5 \
  --label-mode regression
```

## CLI 用法

### Sandbox CLI (`main.py`)

```bash
# 执行指标代码
python sandbox/main.py execute \
  --code 'import pandas as pd; output = {"plots": [], "signals": []}' \
  --df-json '[{"open":1,"high":2,"low":0.5,"close":1.5,"volume":100}]' \
  --params '{"period": 14}' \
  --timeout 20

# 静态分析代码质量
python sandbox/main.py analyze --code 'your indicator code here'
```

### Training CLI (`train.py`)

```bash
python sandbox/train.py train \
  --data ./data/ohlcv.json      # JSON 文件、CSV 文件或内联 JSON
  --model-type lightgbm         # lightgbm 或 xgboost
  --output ./models/my_model    # 输出路径（不含扩展名）
  --horizon 5                   # 预测 N 根 K 线后的收益
  --threshold 0.01              # 分类阈值（1%）
  --label-mode regression       # regression / classification / multi_class
  --n-estimators 100            # 树的数量
  --max-depth 6                 # 最大深度
  --train-split 0.8             # 训练集比例
```

训练完成后会生成两个文件：
- `{output}.pkl` — 序列化模型文件
- `{output}_trees.json` — JSON 树结构（供 Go 端原生推理）

## Docker 构建

```bash
# 构建 sandbox 镜像（用于离线训练环境）
docker build -t xiaotian-sandbox ./sandbox

# 运行训练
docker run --rm -v $(pwd)/data:/data -v $(pwd)/models:/models xiaotian-sandbox \
  python train.py train --data /data/ohlcv.json --output /models/my_model
```

## 依赖

Python 3.12 + pandas + numpy + scikit-learn + lightgbm + xgboost

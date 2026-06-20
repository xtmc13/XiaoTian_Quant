"""XiaoTianQuant ML Training CLI
训练模型并导出 JSON 树供 Go 端原生推理。
"""
import argparse
import json
import pickle
import sys
import os

# 确保 ml_server 目录在路径中（支持从 sandbox/ 或 ml_server/ 运行）
_current_dir = os.path.dirname(os.path.abspath(__file__))
if _current_dir not in sys.path:
    sys.path.insert(0, _current_dir)

from data_kitchen import DataKitchen
from feature_engine import FeatureEngine
from label_creator import LabelCreator
from models.base_model import create_model


def _load_data(data_arg):
    """Load OHLCV data from a JSON string, JSON file, or CSV file."""
    if not data_arg:
        raise ValueError("No data provided")

    # If it's a file path
    if os.path.isfile(data_arg):
        if data_arg.lower().endswith(".csv"):
            import pandas as pd
            df = pd.read_csv(data_arg)
            return df.to_dict("records")
        else:
            with open(data_arg, "r") as f:
                return json.load(f)

    # Otherwise treat as inline JSON
    return json.loads(data_arg)


def train(args):
    """训练模型并导出。"""
    # 加载数据
    bars = _load_data(args.data)

    kitchen = DataKitchen()
    df = kitchen.bars_to_dataframe(bars)

    # 特征工程
    engine = FeatureEngine()
    features_df = engine.transform(df)

    # 标签
    label_config = {
        "label_type": args.label_mode,
        "horizon": args.horizon,
        "threshold": args.threshold,
    }
    labeler = LabelCreator(label_config)
    features_df, label_col = labeler.create_labels(features_df)

    # 训练/测试拆分
    train_data, test_data = kitchen.prepare_data(features_df, label_col, args.train_split)

    # 创建并训练模型
    model_params = {
        "n_estimators": args.n_estimators,
        "max_depth": args.max_depth,
    }

    # Map label_mode to task_type for model creation
    task_type = "regression" if args.label_mode == "regression" else "classification"
    model = create_model(args.model_type, task_type, model_params)

    metrics = model.train(train_data, test_data)

    # 保存模型
    model_path = args.output + ".pkl"
    with open(model_path, "wb") as f:
        pickle.dump(model.model, f)

    # 导出 JSON 树（供 Go 端推理）
    feature_names = engine.get_feature_names()
    trees_json = model.export_trees(feature_names=feature_names)
    trees_path = args.output + "_trees.json"
    with open(trees_path, "w") as f:
        json.dump(trees_json, f, indent=2)

    print(json.dumps({
        "status": "ok",
        "metrics": metrics,
        "model_path": model_path,
        "trees_path": trees_path,
    }, indent=2))


def main():
    parser = argparse.ArgumentParser(description="XiaoTianQuant ML Training CLI")
    subparsers = parser.add_subparsers(dest="command")

    train_parser = subparsers.add_parser("train", help="Train a model")
    train_parser.add_argument("--data", required=True, help="Path to OHLCV CSV/JSON or inline JSON")
    train_parser.add_argument("--model-type", choices=["lightgbm", "xgboost"], default="lightgbm")
    train_parser.add_argument("--output", required=True, help="Output path (without extension)")
    train_parser.add_argument("--horizon", type=int, default=5)
    train_parser.add_argument("--threshold", type=float, default=0.01)
    train_parser.add_argument("--label-mode", choices=["regression", "classification", "multi_class"], default="regression")
    train_parser.add_argument("--n-estimators", type=int, default=100)
    train_parser.add_argument("--max-depth", type=int, default=6)
    train_parser.add_argument("--train-split", type=float, default=0.8)

    args = parser.parse_args()

    if args.command == "train":
        train(args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()

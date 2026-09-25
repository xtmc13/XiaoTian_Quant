"""XiaoTianQuant ML Server — gateway ml.Client 的 HTTP 对端。

FastAPI 服务（默认 :8001），端点与 gateway/internal/ml/client.go 一一对应：
  GET    /health
  POST   /train                 训练（bars 已含 Go 端特征+label 时直接训练，保证
                                训练/推理特征一致；否则走 FeatureEngine+LabelCreator）
  POST   /predict
  GET    /models                已训练模型列表
  GET    /models/{id}           模型元信息
  DELETE /models/{id}
  GET    /models/{id}/export    导出 JSON 树（Go predictor 原生推理用）
  GET    /models/{id}/importance
  POST   /features/generate

模型落盘: $ML_MODEL_DIR（默认 <ml_server>/trained_models）/<model_id>/
  model.pkl  训练好的模型（pickle）
  trees.json 导出的 JSON 树
  meta.json  元信息（feature_names/metrics/型号/任务类型/样本数）

lightgbm/xgboost 未安装时自动降级 sklearn GBDT（见 models/sklearn_gbdt_model.py）。

启动: python3 server.py 或 uvicorn server:app --host 0.0.0.0 --port 8001
"""
import json
import os
import pickle
import re
import sys
import time
import threading
from typing import Any, Dict, List, Optional

import numpy as np
import pandas as pd
from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel

_current_dir = os.path.dirname(os.path.abspath(__file__))
if _current_dir not in sys.path:
    sys.path.insert(0, _current_dir)

from data_kitchen import DataKitchen
from feature_engine import FeatureEngine
from label_creator import LabelCreator
from models.base_model import create_model
from models.sklearn_gbdt_model import SklearnGBDTModel

MODEL_DIR = os.environ.get("ML_MODEL_DIR", os.path.join(_current_dir, "trained_models"))
os.makedirs(MODEL_DIR, exist_ok=True)

# OHLCV/标签保留列，训练时从 bar dict 里剔除后剩下的数值列即特征列
_RESERVED_COLS = {
    "time", "timestamp", "date", "datetime", "symbol", "interval",
    "open", "high", "low", "close", "volume", "label",
}

app = FastAPI(title="XiaoTianQuant ML Server")
_lock = threading.Lock()  # 模型读写串行化（训练/导出/删除互斥）


# ── 请求/响应模型 ────────────────────────────────────────────────


class TrainRequest(BaseModel):
    model_id: str = ""
    model_type: str = "lightgbm"
    task_type: str = "regression"
    symbol: str = ""
    interval: str = ""
    bars: List[Dict[str, Any]] = []
    feature_config: Optional[Dict[str, Any]] = None
    label_config: Optional[Dict[str, Any]] = None
    model_params: Optional[Dict[str, Any]] = None


class PredictRequest(BaseModel):
    model_id: str
    bars: List[Dict[str, Any]] = []


class FeatureGenRequest(BaseModel):
    bars: List[Dict[str, Any]] = []
    config: Optional[Dict[str, Any]] = None


# ── 模型存取 ────────────────────────────────────────────────────

_SAFE_ID = re.compile(r"^[A-Za-z0-9_.\-]+$")


def _model_dir(model_id: str) -> str:
    if not model_id or not _SAFE_ID.match(model_id):
        raise HTTPException(status_code=400, detail="invalid model_id")
    return os.path.join(MODEL_DIR, model_id)


def _load_meta(model_id: str) -> Dict[str, Any]:
    meta_path = os.path.join(_model_dir(model_id), "meta.json")
    if not os.path.isfile(meta_path):
        raise HTTPException(status_code=404, detail=f"model not found: {model_id}")
    with open(meta_path, "r") as f:
        return json.load(f)


def _load_wrapper(model_id: str):
    pkl_path = os.path.join(_model_dir(model_id), "model.pkl")
    if not os.path.isfile(pkl_path):
        raise HTTPException(status_code=404, detail=f"model not found: {model_id}")
    with open(pkl_path, "rb") as f:
        return pickle.load(f)


def _create_with_fallback(model_type: str, task_type: str, params: Dict[str, Any]):
    """请求的模型类型不可用时降级 sklearn GBDT，返回 (model, actual_type)。"""
    if model_type == "sklearn_gbdt":
        return SklearnGBDTModel(task_type, params), "sklearn_gbdt"
    try:
        return create_model(model_type, task_type, params), model_type
    except ImportError:
        return SklearnGBDTModel(task_type, params), "sklearn_gbdt"


def _bars_to_frame(bars: List[Dict[str, Any]]) -> pd.DataFrame:
    df = DataKitchen().bars_to_dataframe(bars)
    if df.empty:
        raise HTTPException(status_code=400, detail="no usable bars")
    return df


def _feature_cols(df: pd.DataFrame) -> List[str]:
    cols = []
    for c in df.columns:
        if c in _RESERVED_COLS:
            continue
        if df[c].dtype in (np.float64, np.float32, np.int64, np.int32, np.bool_, bool):
            cols.append(c)
    return cols


# ── 端点 ────────────────────────────────────────────────────────


@app.get("/health")
def health():
    return {"status": "ok", "model_dir": MODEL_DIR, "time": int(time.time())}


@app.post("/train")
def train(req: TrainRequest):
    started = time.time()
    with _lock:
        df = _bars_to_frame(req.bars)

        if "label" in df.columns:
            # Go pipeline 路径：bars 已含特征列 + label，直接训练（训练/推理特征一致）
            label_col = "label"
            feature_names = _feature_cols(df)
            if not feature_names:
                raise HTTPException(status_code=400, detail="no feature columns in bars")
        else:
            # 裸 OHLCV 路径：本地特征工程 + 标签
            engine = FeatureEngine(req.feature_config)
            df = engine.transform(df)
            labeler = LabelCreator(req.label_config)
            df, label_col = labeler.create_labels(df)
            feature_names = engine.get_feature_names()

        df = df.dropna(subset=[label_col])
        if len(df) < 20:
            raise HTTPException(status_code=400, detail=f"insufficient samples: {len(df)}")

        X = df[feature_names].values.astype(np.float64)
        X = np.nan_to_num(X, nan=0.0, posinf=0.0, neginf=0.0)
        y = df[label_col].values.astype(np.float64)

        # 时序数据按时间顺序切分，不 shuffle；树模型不需要标准化
        split_ratio = 0.8
        if req.label_config and isinstance(req.label_config.get("train_split"), (int, float)):
            split_ratio = float(req.label_config["train_split"])
        split_idx = max(1, int(len(X) * split_ratio))
        train_data = {"X": X[:split_idx], "y": y[:split_idx]}
        test_data = {"X": X[split_idx:], "y": y[split_idx:]}
        if len(test_data["X"]) == 0:  # 样本太少时测试集复用训练集，仅为了产出指标
            test_data = train_data

        model_id = req.model_id or f"{req.symbol or 'model'}_{req.interval or '1h'}_{int(time.time())}"
        model, actual_type = _create_with_fallback(req.model_type, req.task_type, req.model_params or {})

        try:
            metrics = model.train(train_data, test_data)
        except Exception as e:
            raise HTTPException(status_code=500, detail=f"train failed: {e}")

        # 落盘：pkl + trees.json + meta.json
        out_dir = _model_dir(model_id)
        os.makedirs(out_dir, exist_ok=True)
        with open(os.path.join(out_dir, "model.pkl"), "wb") as f:
            pickle.dump(model, f)

        trees = model.export_trees(feature_names) or []
        with open(os.path.join(out_dir, "trees.json"), "w") as f:
            json.dump(trees, f)

        meta = {
            "model_id": model_id,
            "model_type": actual_type,
            "task_type": req.task_type,
            "symbol": req.symbol,
            "interval": req.interval,
            "trained_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "metrics": metrics,
            "feature_count": len(feature_names),
            "feature_names": feature_names,
            "train_samples": int(len(train_data["X"])),
            "test_samples": int(len(test_data["X"])),
            "duration_ms": int((time.time() - started) * 1000),
        }
        with open(os.path.join(out_dir, "meta.json"), "w") as f:
            json.dump(meta, f, indent=2)

        return {
            "success": True,
            "model_id": model_id,
            "model_type": actual_type,
            "metrics": metrics,
            "feature_count": len(feature_names),
            "train_samples": meta["train_samples"],
            "test_samples": meta["test_samples"],
        }


@app.post("/predict")
def predict(req: PredictRequest):
    with _lock:
        meta = _load_meta(req.model_id)
        model = _load_wrapper(req.model_id)

    df = _bars_to_frame(req.bars)
    feature_names = meta.get("feature_names") or []
    if not feature_names:
        raise HTTPException(status_code=500, detail="model meta missing feature_names")

    # 缺列补 0（与 Go predictor PredictFromMap 的缺省语义一致），多列忽略
    for name in feature_names:
        if name not in df.columns:
            df[name] = 0.0
    X = df[feature_names].values.astype(np.float64)
    X = np.nan_to_num(X, nan=0.0, posinf=0.0, neginf=0.0)
    if len(X) == 0:
        raise HTTPException(status_code=400, detail="no rows to predict")

    preds = model.predict(X[-1:])
    value = float(np.ravel(preds)[0])
    return {
        "success": True,
        "model_id": req.model_id,
        "prediction": value,
        "direction": "up" if value > 0 else ("down" if value < 0 else "flat"),
        "strength": min(abs(value) * 100, 1.0),
    }


@app.get("/models")
def list_models():
    models = []
    if os.path.isdir(MODEL_DIR):
        for name in sorted(os.listdir(MODEL_DIR)):
            meta_path = os.path.join(MODEL_DIR, name, "meta.json")
            if not os.path.isfile(meta_path):
                continue
            try:
                with open(meta_path, "r") as f:
                    meta = json.load(f)
                models.append({
                    "model_id": meta.get("model_id", name),
                    "model_type": meta.get("model_type", ""),
                    "task_type": meta.get("task_type", ""),
                    "trained_at": meta.get("trained_at", ""),
                    "metrics": meta.get("metrics", {}),
                    "feature_count": meta.get("feature_count", 0),
                })
            except Exception:
                continue
    return {"models": models}


@app.get("/models/{model_id}")
def get_model(model_id: str):
    return _load_meta(model_id)


@app.delete("/models/{model_id}")
def delete_model(model_id: str):
    import shutil

    with _lock:
        out_dir = _model_dir(model_id)
        if not os.path.isdir(out_dir):
            raise HTTPException(status_code=404, detail=f"model not found: {model_id}")
        shutil.rmtree(out_dir)
    return {"success": True, "model_id": model_id}


@app.get("/models/{model_id}/export")
def export_model(model_id: str):
    meta = _load_meta(model_id)
    trees_path = os.path.join(_model_dir(model_id), "trees.json")
    trees = []
    if os.path.isfile(trees_path):
        with open(trees_path, "r") as f:
            trees = json.load(f)
    if not trees:
        return JSONResponse(status_code=200, content={
            "success": False, "model_id": model_id,
            "error": "model has no exported trees (not exportable or trained before export support)",
        })
    return {
        "success": True,
        "model_id": model_id,
        "model_type": meta.get("model_type", ""),
        "task_type": meta.get("task_type", ""),
        "feature_names": meta.get("feature_names", []),
        "trees": trees,
    }


@app.get("/models/{model_id}/importance")
def feature_importance(model_id: str):
    meta = _load_meta(model_id)
    with _lock:
        model = _load_wrapper(model_id)
    importance = model.get_feature_importance(meta.get("feature_names") or [])
    return {"success": True, "model_id": model_id, "importance": importance}


@app.post("/features/generate")
def generate_features(req: FeatureGenRequest):
    df = _bars_to_frame(req.bars)
    engine = FeatureEngine(req.config)
    features_df = engine.transform(df)
    return {
        "success": True,
        "feature_count": len(engine.get_feature_names()),
        "feature_names": engine.get_feature_names(),
        "sample_count": int(len(features_df)),
    }


if __name__ == "__main__":
    import argparse

    import uvicorn

    parser = argparse.ArgumentParser(description="XiaoTianQuant ML Server")
    parser.add_argument("--host", default=os.environ.get("ML_SERVER_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("ML_SERVER_PORT", "8001")))
    args = parser.parse_args()
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")

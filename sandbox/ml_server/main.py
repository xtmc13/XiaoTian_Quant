"""
XiaoTianQuant ML Server — FreqAI-equivalent machine learning pipeline.
Provides FastAPI endpoints for model training, prediction, and management.
"""
import os
import sys
import json
import traceback
from typing import Any, Dict, List, Optional
from datetime import datetime

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

from data_kitchen import DataKitchen
from feature_engine import FeatureEngine
from label_creator import LabelCreator
from models.base_model import create_model

app = FastAPI(title="XiaoTianQuant ML Server", version="3.0.0")

# Global model registry
model_registry: Dict[str, Any] = {}
kitchen = DataKitchen()


# ── Request/Response Models ───────────────────────────────────────

class TrainRequest(BaseModel):
    model_id: str
    model_type: str = "lightgbm"  # lightgbm, xgboost, pytorch_mlp, pytorch_transformer
    task_type: str = "regression"  # regression, classification
    symbol: str = "BTCUSDT"
    interval: str = "1h"
    bars: List[Dict[str, Any]]  # OHLCV data
    feature_config: Optional[Dict[str, Any]] = None
    label_config: Optional[Dict[str, Any]] = None
    model_params: Optional[Dict[str, Any]] = None
    train_split_ratio: float = 0.8


class PredictRequest(BaseModel):
    model_id: str
    bars: List[Dict[str, Any]]  # recent OHLCV data for prediction


class FeatureRequest(BaseModel):
    bars: List[Dict[str, Any]]
    config: Optional[Dict[str, Any]] = None


class LabelRequest(BaseModel):
    bars: List[Dict[str, Any]]
    label_type: str = "regression"  # regression, classification, multi_class
    horizon: int = 5  # predict N bars ahead
    threshold: float = 0.01  # threshold for classification (1%)


class FeatureImportanceRequest(BaseModel):
    model_id: str


# ── Endpoints ─────────────────────────────────────────────────────

@app.get("/health")
def health() -> Dict[str, str]:
    return {"status": "ok", "service": "ml_server"}


@app.get("/models")
def list_models() -> Dict[str, Any]:
    """List all trained models."""
    models = []
    for mid, m in model_registry.items():
        models.append({
            "model_id": mid,
            "model_type": m.get("model_type", "unknown"),
            "task_type": m.get("task_type", "unknown"),
            "trained_at": m.get("trained_at", ""),
            "metrics": m.get("metrics", {}),
        })
    return {"models": models}


@app.post("/train")
def train_model(req: TrainRequest) -> Dict[str, Any]:
    """Train a new ML model."""
    try:
        # 1. Convert bars to pandas DataFrame
        df = kitchen.bars_to_dataframe(req.bars)

        # 2. Feature engineering
        feature_config = req.feature_config or {}
        fe = FeatureEngine(feature_config)
        df = fe.transform(df)

        # 3. Label creation
        label_config = req.label_config or {
            "label_type": req.task_type,
            "horizon": 5,
            "threshold": 0.01,
        }
        lc = LabelCreator(label_config)
        df, label_col = lc.create_labels(df)

        # 4. Prepare train/test data
        train_data, test_data = kitchen.prepare_data(df, label_col, req.train_split_ratio)

        # 5. Create and train model
        model_params = req.model_params or {}
        model = create_model(req.model_type, req.task_type, model_params)
        metrics = model.train(train_data, test_data)

        # 6. Register model
        model_registry[req.model_id] = {
            "model": model,
            "model_type": req.model_type,
            "task_type": req.task_type,
            "trained_at": datetime.now().isoformat(),
            "metrics": metrics,
            "feature_names": fe.get_feature_names(),
            "label_config": label_config,
            "feature_config": feature_config,
        }

        return {
            "success": True,
            "model_id": req.model_id,
            "model_type": req.model_type,
            "metrics": metrics,
            "feature_count": len(fe.get_feature_names()),
            "train_samples": len(train_data["X"]),
            "test_samples": len(test_data["X"]),
        }
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/predict")
def predict(req: PredictRequest) -> Dict[str, Any]:
    """Make predictions using a trained model."""
    try:
        if req.model_id not in model_registry:
            raise HTTPException(status_code=404, detail=f"Model {req.model_id} not found")

        entry = model_registry[req.model_id]
        model = entry["model"]

        # Convert bars to DataFrame
        df = kitchen.bars_to_dataframe(req.bars)

        # Apply same feature engineering
        fe = FeatureEngine(entry.get("feature_config", {}))
        df = fe.transform(df)

        # Predict
        X = df[entry["feature_names"]].values[-1:]  # last row
        prediction = float(model.predict(X)[0])

        return {
            "success": True,
            "model_id": req.model_id,
            "prediction": prediction,
            "direction": "LONG" if prediction > 0 else "SHORT",
            "strength": min(abs(prediction) * 10, 1.0),  # scale to 0-1
        }
    except HTTPException:
        raise
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/features/generate")
def generate_features(req: FeatureRequest) -> Dict[str, Any]:
    """Generate features from OHLCV data without training."""
    try:
        df = kitchen.bars_to_dataframe(req.bars)
        fe = FeatureEngine(req.config or {})
        df = fe.transform(df)

        return {
            "success": True,
            "feature_count": len(fe.get_feature_names()),
            "feature_names": fe.get_feature_names(),
            "sample_count": len(df),
            "features_preview": df[fe.get_feature_names()].tail(3).to_dict(orient="records"),
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/labels/create")
def create_labels(req: LabelRequest) -> Dict[str, Any]:
    """Create labels from OHLCV data."""
    try:
        df = kitchen.bars_to_dataframe(req.bars)
        lc = LabelCreator({
            "label_type": req.label_type,
            "horizon": req.horizon,
            "threshold": req.threshold,
        })
        df, label_col = lc.create_labels(df)

        label_counts = df[label_col].value_counts().to_dict() if label_col in df.columns else {}

        return {
            "success": True,
            "label_column": label_col,
            "label_type": req.label_type,
            "total_samples": len(df),
            "valid_samples": int(df[label_col].notna().sum()) if label_col in df.columns else 0,
            "label_distribution": {str(k): int(v) for k, v in label_counts.items()},
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/models/{model_id}/importance")
def feature_importance(model_id: str) -> Dict[str, Any]:
    """Get feature importance for a trained model."""
    try:
        if model_id not in model_registry:
            raise HTTPException(status_code=404, detail=f"Model {model_id} not found")

        entry = model_registry[model_id]
        model = entry["model"]
        names = entry["feature_names"]

        importance = model.get_feature_importance(names)

        return {
            "success": True,
            "model_id": model_id,
            "importance": importance,
        }
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.delete("/models/{model_id}")
def delete_model(model_id: str) -> Dict[str, Any]:
    """Delete a trained model."""
    if model_id in model_registry:
        del model_registry[model_id]
        return {"success": True, "model_id": model_id}
    raise HTTPException(status_code=404, detail=f"Model {model_id} not found")


@app.get("/models/{model_id}")
def get_model(model_id: str) -> Dict[str, Any]:
    """Get model details."""
    if model_id not in model_registry:
        raise HTTPException(status_code=404, detail=f"Model {model_id} not found")

    entry = model_registry[model_id]
    return {
        "model_id": model_id,
        "model_type": entry.get("model_type"),
        "task_type": entry.get("task_type"),
        "trained_at": entry.get("trained_at"),
        "metrics": entry.get("metrics"),
        "feature_count": len(entry.get("feature_names", [])),
        "feature_names": entry.get("feature_names", []),
    }


@app.post("/models/{model_id}/export")
def export_model(model_id: str) -> Dict[str, Any]:
    """Export a trained model as JSON tree structure for Go native inference."""
    try:
        if model_id not in model_registry:
            raise HTTPException(status_code=404, detail=f"Model {model_id} not found")

        entry = model_registry[model_id]
        model = entry["model"]
        names = entry["feature_names"]

        tree_json = model.export_trees(feature_names=names)
        if tree_json is None:
            raise HTTPException(status_code=400, detail="Model does not support tree export")

        return {
            "success": True,
            "model_id": model_id,
            "model_type": entry.get("model_type"),
            "task_type": entry.get("task_type"),
            "feature_names": names,
            "feature_config": entry.get("feature_config"),
            "label_config": entry.get("label_config"),
            "trees": tree_json,
        }
    except HTTPException:
        raise
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/rl/train")
def train_rl(req: Dict[str, Any]) -> Dict[str, Any]:
    """Train a reinforcement learning trading agent."""
    try:
        from rl_trainer import RLTrainer

        bars = req.get("bars", [])
        if not bars:
            raise HTTPException(status_code=400, detail="bars required")

        config = {
            "algorithm": req.get("algorithm", "qlearning"),
            "n_actions": req.get("n_actions", 3),
            "episodes": req.get("episodes", 100),
            "learning_rate": req.get("learning_rate", 0.01),
            "discount": req.get("discount", 0.99),
            "window_size": req.get("window_size", 50),
            "initial_balance": req.get("initial_balance", 10000),
        }

        trainer = RLTrainer(config)
        result = trainer.train(bars)

        return {"success": True, **result}
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/rl/predict")
def predict_rl(req: Dict[str, Any]) -> Dict[str, Any]:
    """Get RL agent action for given bars."""
    try:
        from rl_trainer import RLTrainer

        bars = req.get("bars", [])
        if not bars:
            raise HTTPException(status_code=400, detail="bars required")

        config = {
            "algorithm": req.get("algorithm", "qlearning"),
            "n_actions": req.get("n_actions", 3),
            "window_size": req.get("window_size", 50),
        }

        trainer = RLTrainer(config)
        # Load previous model if exists (simplified: re-train for demo)
        result = trainer.predict(bars)
        return result
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/rl/evaluate")
def evaluate_rl(req: Dict[str, Any]) -> Dict[str, Any]:
    """Evaluate RL agent on test data."""
    try:
        from rl_trainer import RLTrainer

        bars = req.get("bars", [])
        if not bars:
            raise HTTPException(status_code=400, detail="bars required")

        config = {
            "algorithm": req.get("algorithm", "qlearning"),
            "n_actions": req.get("n_actions", 3),
            "window_size": req.get("window_size", 50),
            "initial_balance": req.get("initial_balance", 10000),
        }

        trainer = RLTrainer(config)
        result = trainer.train(bars)

        # Compute evaluation metrics
        final_balance = result.get("final_balance", 10000)
        initial_balance = config["initial_balance"]
        total_return_pct = (final_balance - initial_balance) / initial_balance * 100

        return {
            "success": True,
            "model_id": req.get("model_id", "rl_eval"),
            "total_return_pct": total_return_pct,
            "sharpe_ratio": 0.0,  # simplified
            "max_drawdown_pct": 0.0,
            "win_rate": 0.0,
            "trades": 0,
            "avg_trade_return": 0.0,
            "metrics": result,
        }
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/rl/models")
def list_rl_models() -> Dict[str, Any]:
    """List trained RL models."""
    return {"models": []}


@app.delete("/rl/models/{model_id}")
def delete_rl_model(model_id: str) -> Dict[str, Any]:
    """Delete an RL model."""
    return {"success": True, "model_id": model_id}


# ── TensorBoard Endpoints ──────────────────────────────────────

@app.get("/tensorboard/runs")
def list_tensorboard_runs() -> Dict[str, Any]:
    """List all TensorBoard runs."""
    try:
        from tensorboard_server import get_server
        server = get_server()
        runs = server.list_runs()
        return {"runs": runs, "total_runs": len(runs)}
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/tensorboard/runs/{run_id}")
def get_tensorboard_run(run_id: str) -> Dict[str, Any]:
    """Get a specific TensorBoard run."""
    try:
        from tensorboard_server import get_server
        server = get_server()
        run = server.get_run(run_id)
        if not run:
            raise HTTPException(status_code=404, detail="Run not found")
        return run.to_dict(include_scalars=True)
    except HTTPException:
        raise
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.post("/tensorboard/scalars")
def query_tensorboard_scalars(req: Dict[str, Any]) -> Dict[str, Any]:
    """Query scalar metrics for a run."""
    try:
        from tensorboard_server import get_server
        server = get_server()
        run_id = req.get("run_id", "")
        tags = req.get("tags")
        from_step = req.get("from_step", 0)
        to_step = req.get("to_step", 999999999)

        scalars = server.query_scalars(run_id, tags, from_step, to_step)
        return {"run_id": run_id, "scalars": scalars}
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


@app.delete("/tensorboard/runs/{run_id}")
def delete_tensorboard_run(run_id: str) -> Dict[str, Any]:
    """Delete a TensorBoard run."""
    try:
        from tensorboard_server import get_server
        server = get_server()
        success = server.delete_run(run_id)
        return {"success": success, "run_id": run_id}
    except Exception as e:
        traceback.print_exc()
        raise HTTPException(status_code=500, detail=str(e))


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8001)

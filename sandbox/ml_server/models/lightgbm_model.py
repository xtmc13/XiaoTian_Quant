"""
LightGBM Model — Gradient boosting for regression and classification.
Aligns with FreqAI LightGBMRegressor / LightGBMClassifier.
"""
import numpy as np
from typing import Any, Dict, List, Optional
from models.base_model import BaseModel

try:
    import lightgbm as lgb
    HAS_LIGHTGBM = True
except ImportError:
    HAS_LIGHTGBM = False


class LightGBMModel(BaseModel):
    """LightGBM prediction model."""

    def __init__(self, task_type: str = "regression", params: Optional[Dict[str, Any]] = None):
        super().__init__(task_type, params)

    def _create_model(self):
        if not HAS_LIGHTGBM:
            raise ImportError("lightgbm is not installed. Run: pip install lightgbm")

        default_params = {
            "n_estimators": self.params.get("n_estimators", 200),
            "max_depth": self.params.get("max_depth", 6),
            "learning_rate": self.params.get("learning_rate", 0.05),
            "num_leaves": self.params.get("num_leaves", 31),
            "min_child_samples": self.params.get("min_child_samples", 20),
            "subsample": self.params.get("subsample", 0.8),
            "colsample_bytree": self.params.get("colsample_bytree", 0.8),
            "reg_alpha": self.params.get("reg_alpha", 0.1),
            "reg_lambda": self.params.get("reg_lambda", 0.1),
            "random_state": 42,
            "n_jobs": -1,
            "verbosity": -1,
        }

        if self.task_type == "classification":
            default_params["objective"] = "binary"
            default_params["metric"] = "binary_logloss"
        else:
            default_params["objective"] = "regression"
            default_params["metric"] = "rmse"

        default_params.update({k: v for k, v in self.params.items()
                              if k in default_params})

        self.model = lgb.LGBMRegressor(**default_params) if self.task_type == "regression" \
                else lgb.LGBMClassifier(**default_params)

    def get_feature_importance(self, feature_names: List[str]) -> List[Dict[str, Any]]:
        if self.model is None:
            return []

        importance = self.model.feature_importances_
        pairs = sorted(zip(feature_names, importance), key=lambda x: x[1], reverse=True)
        return [{"name": n, "importance": float(v)} for n, v in pairs]

    def export_trees(self, feature_names: List[str]) -> Optional[List[Dict[str, Any]]]:
        """Export LightGBM model as simplified JSON tree structure for Go inference."""
        if not HAS_LIGHTGBM or self.model is None:
            return None

        try:
            booster = self.model.booster_
            model_dump = booster.dump_model()

            trees = []
            for tree_info in model_dump.get("tree_info", []):
                tree_struct = tree_info.get("tree_structure", {})
                trees.append(self._simplify_tree(tree_struct, feature_names))

            return trees
        except Exception:
            return None

    def _simplify_tree(self, node: dict, feature_names: List[str]) -> dict:
        """Recursively simplify a LightGBM tree node to a minimal format."""
        if "leaf_value" in node:
            return {"leaf": float(node["leaf_value"])}

        feat_idx = node.get("split_feature", 0)
        feat_name = feature_names[feat_idx] if feat_idx < len(feature_names) else f"f{feat_idx}"
        threshold = float(node.get("threshold", 0))

        return {
            "feature": feat_name,
            "threshold": threshold,
            "left": self._simplify_tree(node.get("left_child", {"leaf_value": 0}), feature_names),
            "right": self._simplify_tree(node.get("right_child", {"leaf_value": 0}), feature_names),
        }

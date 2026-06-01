"""
XGBoost Model — Gradient boosting for regression and classification.
Aligns with FreqAI XGBoostRegressor / XGBoostClassifier.
"""
import numpy as np
from typing import Any, Dict, List, Optional
from models.base_model import BaseModel

try:
    import xgboost as xgb
    HAS_XGBOOST = True
except ImportError:
    HAS_XGBOOST = False


class XGBoostModel(BaseModel):
    """XGBoost prediction model."""

    def __init__(self, task_type: str = "regression", params: Optional[Dict[str, Any]] = None):
        super().__init__(task_type, params)

    def _create_model(self):
        if not HAS_XGBOOST:
            raise ImportError("xgboost is not installed. Run: pip install xgboost")

        default_params = {
            "n_estimators": self.params.get("n_estimators", 200),
            "max_depth": self.params.get("max_depth", 6),
            "learning_rate": self.params.get("learning_rate", 0.05),
            "subsample": self.params.get("subsample", 0.8),
            "colsample_bytree": self.params.get("colsample_bytree", 0.8),
            "reg_alpha": self.params.get("reg_alpha", 0.1),
            "reg_lambda": self.params.get("reg_lambda", 0.1),
            "random_state": 42,
            "n_jobs": -1,
            "verbosity": 0,
        }

        if self.task_type == "classification":
            default_params["objective"] = "binary:logistic"
            default_params["eval_metric"] = "logloss"
        else:
            default_params["objective"] = "reg:squarederror"
            default_params["eval_metric"] = "rmse"

        default_params.update({k: v for k, v in self.params.items()
                              if k in default_params})

        self.model = xgb.XGBRegressor(**default_params) if self.task_type == "regression" \
                else xgb.XGBClassifier(**default_params)

    def get_feature_importance(self, feature_names: List[str]) -> List[Dict[str, Any]]:
        if self.model is None:
            return []

        importance = self.model.feature_importances_
        pairs = sorted(zip(feature_names, importance), key=lambda x: x[1], reverse=True)
        return [{"name": n, "importance": float(v)} for n, v in pairs]

    def export_trees(self, feature_names: List[str]) -> Optional[List[Dict[str, Any]]]:
        """Export XGBoost model as simplified JSON tree structure for Go inference."""
        if not HAS_XGBOOST or self.model is None:
            return None

        try:
            booster = self.model.get_booster()
            model_dump = booster.get_dump(dump_format="json")

            import json
            trees = []
            for tree_str in model_dump:
                tree = json.loads(tree_str)
                # XGBoost format: recursive dict with keys: nodeid, depth, split, split_condition, yes, no, children, leaf
                trees.append(self._simplify_xgb_tree(tree, feature_names))

            return trees
        except Exception:
            return None

    def _simplify_xgb_tree(self, node: dict, feature_names: List[str]) -> dict:
        """Recursively simplify an XGBoost tree node."""
        if "leaf" in node:
            return {"leaf": float(node["leaf"])}

        feat_name = node.get("split", "unknown")
        threshold = float(node.get("split_condition", 0))

        children = node.get("children", [])
        left_child = children[0] if len(children) > 0 else {"leaf": 0}
        right_child = children[1] if len(children) > 1 else {"leaf": 0}

        return {
            "feature": feat_name,
            "threshold": threshold,
            "left": self._simplify_xgb_tree(left_child, feature_names),
            "right": self._simplify_xgb_tree(right_child, feature_names),
        }

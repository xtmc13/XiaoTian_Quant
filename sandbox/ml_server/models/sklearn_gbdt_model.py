"""
Sklearn GBDT Model — lightgbm/xgboost 不可用时的降级模型。
与 BaseModel 同接口，支持 export_trees（Go 端原生推理）。
sklearn GBDT 的 learning_rate 在 predict 时乘在树输出上，
导出时把 learning_rate 折进叶值、init 折进首棵常数树，保证 Go 端
predictor 的"树值求和"语义与 sklearn predict 一致（回归任务）。
"""
from typing import Any, Dict, List, Optional

import numpy as np
from sklearn.ensemble import GradientBoostingClassifier, GradientBoostingRegressor

from models.base_model import BaseModel


class SklearnGBDTModel(BaseModel):
    """Gradient boosting based on sklearn (fallback for lightgbm/xgboost)."""

    def __init__(self, task_type: str = "regression", params: Optional[Dict[str, Any]] = None):
        super().__init__(task_type, params)
        self._init_value = 0.0

    def _create_model(self):
        default_params = {
            "n_estimators": self.params.get("n_estimators", 100),
            "max_depth": self.params.get("max_depth", 3),
            "learning_rate": self.params.get("learning_rate", 0.1),
            "subsample": self.params.get("subsample", 1.0),
            "random_state": 42,
        }
        if self.task_type == "classification":
            self.model = GradientBoostingClassifier(**default_params)
        else:
            self.model = GradientBoostingRegressor(**default_params)

    def train(self, train_data, test_data):
        metrics = super().train(train_data, test_data)
        # 记录 init 基线（回归为 y 均值），导出时折进首棵常数树
        try:
            init_est = getattr(self.model, "init_", None)
            if init_est is not None:
                self._init_value = float(np.ravel(init_est.constant_)[0])
        except Exception:
            self._init_value = 0.0
        return metrics

    def get_feature_importance(self, feature_names: List[str]) -> List[Dict[str, Any]]:
        if self.model is None:
            return []
        importance = self.model.feature_importances_
        pairs = sorted(zip(feature_names, importance), key=lambda x: x[1], reverse=True)
        return [{"name": n, "importance": float(v)} for n, v in pairs]

    def export_trees(self, feature_names: List[str]) -> Optional[List[Dict[str, Any]]]:
        """Export sklearn GBDT as simplified JSON trees for Go inference."""
        if self.model is None:
            return None
        try:
            lr = float(getattr(self.model, "learning_rate", 1.0))
            estimators = self.model.estimators_
            trees: List[Dict[str, Any]] = []
            # 首棵：常数树携带 init 基线（Go predictor BaseScore=0）
            if self._init_value != 0.0:
                trees.append({"leaf": self._init_value})
            for row in estimators:
                # 分类多输出时 estimators_ 形状为 (n_estimators, n_classes)，取第 0 类
                tree = row[0] if isinstance(row, (list, np.ndarray)) else row
                trees.append(self._simplify_tree(tree.tree_, 0, feature_names, lr))
            return trees
        except Exception:
            return None

    def _simplify_tree(self, t, node_idx: int, feature_names: List[str], lr: float) -> dict:
        left = int(t.children_left[node_idx])
        right = int(t.children_right[node_idx])
        if left == -1 and right == -1:
            return {"leaf": float(np.ravel(t.value[node_idx])[0]) * lr}
        feat_idx = int(t.feature[node_idx])
        feat_name = feature_names[feat_idx] if 0 <= feat_idx < len(feature_names) else f"f{feat_idx}"
        return {
            "feature": feat_name,
            "threshold": float(t.threshold[node_idx]),
            "left": self._simplify_tree(t, left, feature_names, lr),
            "right": self._simplify_tree(t, right, feature_names, lr),
        }

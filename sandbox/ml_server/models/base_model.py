"""
Base model interface for all ML models.
Aligns with FreqAI BaseRegressionModel / BaseClassifierModel.
"""
from abc import ABC, abstractmethod
import numpy as np
from typing import Any, Dict, List, Optional
from sklearn.metrics import (
    mean_squared_error, mean_absolute_error, r2_score,
    accuracy_score, precision_score, recall_score, f1_score,
)


class BaseModel(ABC):
    """Abstract base class for all prediction models."""

    def __init__(self, task_type: str = "regression", params: Optional[Dict[str, Any]] = None):
        self.task_type = task_type  # regression, classification
        self.params = params or {}
        self.model: Any = None

    @abstractmethod
    def _create_model(self):
        """Create the underlying model instance."""
        pass

    def train(self, train_data: Dict[str, np.ndarray],
              test_data: Dict[str, np.ndarray]) -> Dict[str, Any]:
        """Train the model and return performance metrics."""
        X_train, y_train = train_data["X"], train_data["y"]
        X_test, y_test = test_data["X"], test_data["y"]

        self._create_model()
        self.model.fit(X_train, y_train)

        # Predict
        y_pred_train = self.model.predict(X_train)
        y_pred_test = self.model.predict(X_test)

        metrics = self._compute_metrics(y_train, y_pred_train, y_test, y_pred_test)
        return metrics

    def predict(self, X: np.ndarray) -> np.ndarray:
        """Make predictions."""
        if self.model is None:
            raise RuntimeError("Model not trained. Call train() first.")
        return self.model.predict(X)

    def _compute_metrics(self, y_train, y_pred_train, y_test, y_pred_test) -> Dict[str, Any]:
        """Compute evaluation metrics based on task type."""
        metrics = {}

        if self.task_type == "regression":
            metrics["train_mse"] = float(mean_squared_error(y_train, y_pred_train))
            metrics["test_mse"] = float(mean_squared_error(y_test, y_pred_test))
            metrics["train_rmse"] = float(np.sqrt(metrics["train_mse"]))
            metrics["test_rmse"] = float(np.sqrt(metrics["test_mse"]))
            metrics["train_mae"] = float(mean_absolute_error(y_train, y_pred_train))
            metrics["test_mae"] = float(mean_absolute_error(y_test, y_pred_test))
            metrics["train_r2"] = float(r2_score(y_train, y_pred_train))
            metrics["test_r2"] = float(r2_score(y_test, y_pred_test))
        else:
            # Classification
            y_pred_train_class = np.round(y_pred_train).astype(int)
            y_pred_test_class = np.round(y_pred_test).astype(int)
            y_train_class = y_train.astype(int)
            y_test_class = y_test.astype(int)

            metrics["train_accuracy"] = float(accuracy_score(y_train_class, y_pred_train_class))
            metrics["test_accuracy"] = float(accuracy_score(y_test_class, y_pred_test_class))
            metrics["test_precision"] = float(precision_score(y_test_class, y_pred_test_class,
                                                average="weighted", zero_division=0))
            metrics["test_recall"] = float(recall_score(y_test_class, y_pred_test_class,
                                           average="weighted", zero_division=0))
            metrics["test_f1"] = float(f1_score(y_test_class, y_pred_test_class,
                                       average="weighted", zero_division=0))

        return metrics

    @abstractmethod
    def get_feature_importance(self, feature_names: List[str]) -> List[Dict[str, Any]]:
        """Return feature importance as [{name, importance}, ...] sorted descending."""
        pass

    def export_trees(self, feature_names: List[str]) -> Optional[List[Dict[str, Any]]]:
        """Export model as JSON tree structure for Go native inference.
        Returns list of tree dicts, or None if not supported."""
        return None


def create_model(model_type: str, task_type: str,
                 params: Optional[Dict[str, Any]] = None) -> BaseModel:
    """Factory function to create a model by type."""
    if model_type == "lightgbm":
        from models.lightgbm_model import LightGBMModel
        return LightGBMModel(task_type, params)
    elif model_type == "xgboost":
        from models.xgboost_model import XGBoostModel
        return XGBoostModel(task_type, params)
    else:
        raise ValueError(f"Unknown model type: {model_type}. Available: lightgbm, xgboost")

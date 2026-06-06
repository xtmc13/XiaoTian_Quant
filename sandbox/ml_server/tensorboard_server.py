"""
TensorBoard Server — Lightweight metrics storage and query service.
Provides REST API for scalar logging, run management, and data retrieval.
Compatible with the Go gateway's TensorBoardClient.
"""
import json
import os
import time
from typing import Any, Dict, List, Optional
from datetime import datetime

# Try to import tensorboard for native support
try:
    from tensorboard.backend.event_processing import event_accumulator
    from tensorboard.summary.v1 import scalar as tb_scalar
    HAS_TENSORBOARD = True
except ImportError:
    HAS_TENSORBOARD = False


class TensorBoardRun:
    """Represents a single TensorBoard run with scalar metrics."""

    def __init__(self, run_id: str, run_name: str, model_type: str = "rl", model_id: str = ""):
        self.run_id = run_id
        self.run_name = run_name
        self.model_type = model_type
        self.model_id = model_id
        self.started_at = datetime.now().isoformat()
        self.updated_at = self.started_at
        self.status = "running"
        self.tags: List[str] = []
        self.scalars: Dict[str, List[Dict[str, Any]]] = {}  # tag -> [{step, wall_time, value}]
        self.log_dir = f"./tensorboard_logs/{run_id}"
        os.makedirs(self.log_dir, exist_ok=True)

    def add_scalar(self, tag: str, step: int, value: float) -> None:
        """Add a scalar value to this run."""
        if tag not in self.scalars:
            self.scalars[tag] = []
            if tag not in self.tags:
                self.tags.append(tag)

        self.scalars[tag].append({
            "step": step,
            "wall_time": time.time(),
            "value": float(value),
        })
        self.updated_at = datetime.now().isoformat()

    def to_dict(self, include_scalars: bool = False) -> Dict[str, Any]:
        """Serialize run to dictionary."""
        result = {
            "run_id": self.run_id,
            "run_name": self.run_name,
            "model_type": self.model_type,
            "model_id": self.model_id,
            "started_at": self.started_at,
            "updated_at": self.updated_at,
            "status": self.status,
            "tags": self.tags,
        }
        if include_scalars:
            result["scalars"] = [
                {"tag": tag, "points": points}
                for tag, points in self.scalars.items()
            ]
        return result


class TensorBoardServer:
    """In-memory TensorBoard metrics server with optional disk persistence."""

    def __init__(self, log_dir: str = "./tensorboard_logs"):
        self.log_dir = log_dir
        self.runs: Dict[str, TensorBoardRun] = {}
        os.makedirs(log_dir, exist_ok=True)

    def create_run(self, run_id: str, run_name: str, model_type: str = "rl", model_id: str = "") -> TensorBoardRun:
        """Create a new run."""
        run = TensorBoardRun(run_id, run_name, model_type, model_id)
        self.runs[run_id] = run
        return run

    def get_run(self, run_id: str) -> Optional[TensorBoardRun]:
        """Get a run by ID."""
        return self.runs.get(run_id)

    def add_scalar(self, run_id: str, tag: str, step: int, value: float) -> bool:
        """Add scalar to a run. Creates run if not exists."""
        if run_id not in self.runs:
            self.create_run(run_id, run_id)
        self.runs[run_id].add_scalar(tag, step, value)
        return True

    def list_runs(self) -> List[Dict[str, Any]]:
        """List all runs."""
        return [run.to_dict() for run in self.runs.values()]

    def query_scalars(self, run_id: str, tags: Optional[List[str]] = None,
                      from_step: int = 0, to_step: int = 999999999) -> Dict[str, List[Dict[str, Any]]]:
        """Query scalar data for a run."""
        run = self.runs.get(run_id)
        if not run:
            return {}

        result = {}
        target_tags = tags if tags else run.tags
        for tag in target_tags:
            if tag in run.scalars:
                points = [
                    p for p in run.scalars[tag]
                    if from_step <= p["step"] <= to_step
                ]
                if points:
                    result[tag] = points
        return result

    def delete_run(self, run_id: str) -> bool:
        """Delete a run and its data."""
        if run_id in self.runs:
            del self.runs[run_id]
            # Clean up log directory
            import shutil
            run_dir = os.path.join(self.log_dir, run_id)
            if os.path.exists(run_dir):
                shutil.rmtree(run_dir)
            return True
        return False

    def finish_run(self, run_id: str, status: str = "completed") -> bool:
        """Mark a run as finished."""
        if run_id in self.runs:
            self.runs[run_id].status = status
            self.runs[run_id].updated_at = datetime.now().isoformat()
            return True
        return False


# Global server instance
_tb_server = TensorBoardServer()


def get_server() -> TensorBoardServer:
    """Get the global TensorBoard server instance."""
    return _tb_server


# ── FastAPI/Flask-compatible route handlers ────────────────────

def handle_list_runs() -> Dict[str, Any]:
    """Handler for GET /tensorboard/runs"""
    runs = get_server().list_runs()
    return {
        "runs": runs,
        "total_runs": len(runs),
    }


def handle_get_run(run_id: str) -> Dict[str, Any]:
    """Handler for GET /tensorboard/runs/{run_id}"""
    run = get_server().get_run(run_id)
    if not run:
        return {"error": "Run not found"}, 404
    return run.to_dict(include_scalars=True)


def handle_query_scalars(data: Dict[str, Any]) -> Dict[str, Any]:
    """Handler for POST /tensorboard/scalars"""
    run_id = data.get("run_id", "")
    tags = data.get("tags")
    from_step = data.get("from_step", 0)
    to_step = data.get("to_step", 999999999)

    scalars = get_server().query_scalars(run_id, tags, from_step, to_step)
    return {
        "run_id": run_id,
        "scalars": scalars,
    }


def handle_delete_run(run_id: str) -> Dict[str, Any]:
    """Handler for DELETE /tensorboard/runs/{run_id}"""
    success = get_server().delete_run(run_id)
    return {"success": success, "run_id": run_id}


def handle_add_scalar(data: Dict[str, Any]) -> Dict[str, Any]:
    """Handler for POST /tensorboard/scalar (internal use)"""
    run_id = data.get("run_id", "")
    tag = data.get("tag", "")
    step = data.get("step", 0)
    value = data.get("value", 0.0)

    success = get_server().add_scalar(run_id, tag, step, value)
    return {"success": success}


def handle_finish_run(data: Dict[str, Any]) -> Dict[str, Any]:
    """Handler for POST /tensorboard/finish"""
    run_id = data.get("run_id", "")
    status = data.get("status", "completed")
    success = get_server().finish_run(run_id, status)
    return {"success": success, "run_id": run_id}

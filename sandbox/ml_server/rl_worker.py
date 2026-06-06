"""
Independent RL Worker — Runs PPO/A2C/SAC training outside the HTTP server.
Consumes tasks from Redis (or file-based fallback) and reports results back.
Does NOT use port 8001; communicates via Redis or filesystem.

Usage:
    python rl_worker.py --redis-url redis://localhost:6379/0
    python rl_worker.py --file-mode --queue-dir ./rl_queue
"""
import os
import sys
import json
import time
import argparse
import traceback
import numpy as np
from typing import Any, Dict, List, Optional
from datetime import datetime

# Add parent to path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from rl_trainer import RLTrainer
from tensorboard_server import get_server

# Try Redis
try:
    import redis
    HAS_REDIS = True
except ImportError:
    HAS_REDIS = False


class RedisQueue:
    """Redis-based task queue for RL jobs with heartbeat support."""

    def __init__(self, redis_url: str = "redis://localhost:6379/0"):
        self.client = redis.from_url(redis_url, decode_responses=True)
        self.prefix = "rl:"
        self.queue_key = self.prefix + "queue"
        self.worker_id = f"worker_{os.getpid()}_{int(time.time())}"
        self._ensure_connection()

    def _ensure_connection(self):
        try:
            self.client.ping()
        except Exception as e:
            raise ConnectionError(f"Redis connection failed: {e}")

    def heartbeat(self, status: str = "idle", current_job: Optional[str] = None):
        """Report worker heartbeat to Redis."""
        data = {
            "worker_id": self.worker_id,
            "status": status,
            "current_job": current_job,
            "last_seen": datetime.now().isoformat(),
            "pid": os.getpid(),
        }
        self.client.setex(self.prefix + "worker:" + self.worker_id, 30, json.dumps(data))

    def get_queue_length(self) -> int:
        """Get current queue length."""
        return self.client.llen(self.queue_key)

    def get_all_workers(self) -> List[Dict[str, Any]]:
        """Get all active workers."""
        workers = []
        for key in self.client.scan_iter(match=self.prefix + "worker:*"):
            data = self.client.get(key)
            if data:
                workers.append(json.loads(data))
        return workers

    def dequeue(self, timeout: int = 5) -> Optional[Dict[str, Any]]:
        """Block until a job is available."""
        result = self.client.brpop(self.queue_key, timeout=timeout)
        if not result:
            return None
        job_id = result[1]
        job_data = self.client.get(self.prefix + "job:" + job_id)
        if not job_data:
            return None
        return json.loads(job_data)

    def update_job(self, job_id: str, data: Dict[str, Any]):
        """Update job status in Redis."""
        key = self.prefix + "job:" + job_id
        existing = self.client.get(key)
        if existing:
            merged = json.loads(existing)
            merged.update(data)
            self.client.set(key, json.dumps(merged), ex=86400)
        else:
            self.client.set(key, json.dumps(data), ex=86400)

    def report_progress(self, job_id: str, progress: Dict[str, Any]):
        """Report training progress."""
        self.update_job(job_id, {"progress": progress, "status": "running"})

    def report_completion(self, job_id: str, result: Dict[str, Any]):
        """Report training completion."""
        self.update_job(job_id, {
            "status": "completed",
            "result": result,
            "completed_at": datetime.now().isoformat(),
        })

    def report_failure(self, job_id: str, error: str):
        """Report training failure."""
        self.update_job(job_id, {
            "status": "failed",
            "error": error,
            "completed_at": datetime.now().isoformat(),
        })


class FileQueue:
    """File-based task queue (fallback when Redis is unavailable)."""

    def __init__(self, queue_dir: str = "./rl_queue"):
        self.queue_dir = queue_dir
        self.jobs_dir = os.path.join(queue_dir, "jobs")
        self.pending_dir = os.path.join(queue_dir, "pending")
        self.running_dir = os.path.join(queue_dir, "running")
        self.completed_dir = os.path.join(queue_dir, "completed")
        for d in [self.jobs_dir, self.pending_dir, self.running_dir, self.completed_dir]:
            os.makedirs(d, exist_ok=True)

    def dequeue(self, timeout: int = 5) -> Optional[Dict[str, Any]]:
        """Poll pending directory for new jobs."""
        start = time.time()
        while time.time() - start < timeout:
            pending = sorted(os.listdir(self.pending_dir))
            for job_file in pending:
                job_path = os.path.join(self.pending_dir, job_file)
                try:
                    with open(job_path, 'r', encoding='utf-8') as f:
                        job = json.load(f)
                    # Move to running
                    running_path = os.path.join(self.running_dir, job_file)
                    os.rename(job_path, running_path)
                    job["_file_path"] = running_path
                    return job
                except Exception:
                    continue
            time.sleep(1)
        return None

    def update_job(self, job_id: str, data: Dict[str, Any]):
        """Update job file."""
        for d in [self.running_dir, self.completed_dir]:
            path = os.path.join(d, job_id + ".json")
            if os.path.exists(path):
                with open(path, 'r', encoding='utf-8') as f:
                    existing = json.load(f)
                existing.update(data)
                with open(path, 'w', encoding='utf-8') as f:
                    json.dump(existing, f, ensure_ascii=False)
                return

    def report_progress(self, job_id: str, progress: Dict[str, Any]):
        self.update_job(job_id, {"progress": progress, "status": "running"})

    def report_completion(self, job_id: str, result: Dict[str, Any]):
        self._move_to_completed(job_id)
        self.update_job(job_id, {
            "status": "completed",
            "result": result,
            "completed_at": datetime.now().isoformat(),
        })

    def report_failure(self, job_id: str, error: str):
        self._move_to_completed(job_id)
        self.update_job(job_id, {
            "status": "failed",
            "error": error,
            "completed_at": datetime.now().isoformat(),
        })

    def _move_to_completed(self, job_id: str):
        src = os.path.join(self.running_dir, job_id + ".json")
        dst = os.path.join(self.completed_dir, job_id + ".json")
        if os.path.exists(src):
            os.rename(src, dst)


def create_queue(args) -> Any:
    """Create appropriate queue based on arguments."""
    if args.redis_url and HAS_REDIS:
        try:
            return RedisQueue(args.redis_url)
        except Exception as e:
            print(f"Redis failed: {e}. Falling back to file queue.")
    return FileQueue(args.queue_dir)


def process_job(job: Dict[str, Any], queue: Any) -> None:
    """Process a single RL training job."""
    job_id = job.get("job_id", "unknown")
    algorithm = job.get("algorithm", "qlearning")
    n_actions = job.get("n_actions", 3)
    bars = job.get("bars", [])
    config = job.get("config", {})

    print(f"[{datetime.now().isoformat()}] Processing job {job_id}: {algorithm} ({n_actions} actions, {len(bars)} bars)")

    try:
        if not bars:
            raise ValueError("No bars provided")

        # Build trainer config
        trainer_config = {
            "algorithm": algorithm,
            "n_actions": n_actions,
            "episodes": config.get("episodes", 100),
            "learning_rate": config.get("learning_rate", 0.01),
            "discount": config.get("discount", 0.99),
            "window_size": config.get("window_size", 50),
            "initial_balance": config.get("initial_balance", 10000),
            "commission": config.get("commission", 0.001),
            "run_id": config.get("model_id", job_id),
        }

        # For SB3 algorithms, use total_timesteps instead of episodes
        if algorithm in ["ppo", "a2c", "sac"]:
            trainer_config["total_timesteps"] = config.get("episodes", 100) * 100

        trainer = RLTrainer(trainer_config)

        # Progress callback wrapper
        class ProgressReporter:
            def __init__(self, q, jid):
                self.queue = q
                self.job_id = jid
                self.last_report = 0

            def report(self, episode: int, total: int, **kwargs):
                now = time.time()
                if now - self.last_report < 2:  # throttle to every 2s
                    return
                self.last_report = now
                progress = {
                    "current_episode": episode,
                    "total_episodes": total,
                    "current_step": kwargs.get("step", 0),
                    "total_steps": kwargs.get("total_steps", total * 100),
                    "best_reward": kwargs.get("best_reward", 0.0),
                    "current_balance": kwargs.get("balance", 0.0),
                }
                self.queue.report_progress(self.job_id, progress)
                print(f"  Progress: episode {episode}/{total}")

        reporter = ProgressReporter(queue, job_id)

        # Monkey-patch Q-learning trainer to report progress
        if algorithm == "qlearning":
            original_train = trainer.trainer.train if hasattr(trainer, 'trainer') and trainer.trainer else None
            if original_train:
                def patched_train(env, features, prices):
                    result = original_train(env, features, prices)
                    # Report final progress
                    reporter.report(
                        trainer.config.get("episodes", 100),
                        trainer.config.get("episodes", 100),
                        best_reward=result.get("best_reward", 0),
                        balance=result.get("final_balance", 0),
                    )
                    return result
                trainer.trainer.train = patched_train

        # Run training
        result = trainer.train(bars)

        # Build response
        response = {
            "success": result.get("success", True),
            "algorithm": algorithm,
            "n_actions": n_actions,
            "final_balance": result.get("final_balance", 0.0),
            "total_pnl": result.get("total_pnl", 0.0),
            "best_reward": result.get("best_reward", 0.0),
            "avg_reward_last_10": result.get("avg_reward_last_10", 0.0),
            "q_table_size": result.get("q_table_size", 0),
            "episode_rewards": result.get("episode_rewards", []),
            "total_timesteps": result.get("total_timesteps", 0),
        }

        queue.report_completion(job_id, response)
        print(f"[{datetime.now().isoformat()}] Job {job_id} completed successfully")

    except Exception as e:
        error_msg = str(e)
        traceback_str = traceback.format_exc()
        print(f"[{datetime.now().isoformat()}] Job {job_id} failed: {error_msg}")
        print(traceback_str)
        queue.report_failure(job_id, error_msg)


def main():
    parser = argparse.ArgumentParser(description="XiaoTianQuant RL Worker")
    parser.add_argument("--redis-url", default=os.getenv("REDIS_URL", "redis://localhost:6379/0"),
                        help="Redis URL for task queue")
    parser.add_argument("--queue-dir", default="./rl_queue",
                        help="Directory for file-based queue (fallback)")
    parser.add_argument("--file-mode", action="store_true",
                        help="Force file-based queue mode")
    parser.add_argument("--poll-interval", type=int, default=1,
                        help="Polling interval in seconds")
    parser.add_argument("--max-jobs", type=int, default=0,
                        help="Max jobs to process (0 = unlimited)")
    args = parser.parse_args()

    # Create queue
    if args.file_mode:
        queue = FileQueue(args.queue_dir)
    else:
        queue = create_queue(args)

    print(f"[{datetime.now().isoformat()}] RL Worker started")
    print(f"  Worker ID: {queue.worker_id if hasattr(queue, 'worker_id') else 'file-mode'}")
    print(f"  Queue type: {'Redis' if isinstance(queue, RedisQueue) else 'File'}")
    print(f"  Supported algorithms: ppo, a2c, sac, qlearning")
    print(f"  Waiting for jobs...")

    jobs_processed = 0
    last_heartbeat = 0
    while True:
        if args.max_jobs > 0 and jobs_processed >= args.max_jobs:
            print(f"[{datetime.now().isoformat()}] Max jobs ({args.max_jobs}) reached. Exiting.")
            break

        # Send heartbeat every 5 seconds
        if isinstance(queue, RedisQueue):
            now = time.time()
            if now - last_heartbeat >= 5:
                queue.heartbeat("idle")
                last_heartbeat = now

        try:
            job = queue.dequeue(timeout=args.poll_interval)
            if job is None:
                continue

            # Update heartbeat to running
            if isinstance(queue, RedisQueue):
                queue.heartbeat("running", job.get("job_id"))

            process_job(job, queue)
            jobs_processed += 1

            # Update heartbeat back to idle
            if isinstance(queue, RedisQueue):
                queue.heartbeat("idle")
                last_heartbeat = time.time()

        except KeyboardInterrupt:
            print(f"\n[{datetime.now().isoformat()}] Shutting down...")
            break
        except Exception as e:
            print(f"[{datetime.now().isoformat()}] Worker error: {e}")
            traceback.print_exc()
            time.sleep(5)


if __name__ == "__main__":
    main()

"""
Enhanced RL Trainer with TensorBoard logging and Stable-Baselines3 support.
Extends the base Q-learning with PPO/A2C/SAC via SB3 (if installed).
"""
import json
import numpy as np
import time
from typing import Any, Dict, List, Optional, Tuple

from models.rl_env import TradingEnv3Action, TradingEnv5Action
from tensorboard_server import get_server

# Try to import Stable-Baselines3
try:
    import stable_baselines3 as sb3
    from stable_baselines3 import PPO, A2C, SAC
    from stable_baselines3.common.callbacks import BaseCallback
    HAS_SB3 = True

    class TensorBoardCallback(BaseCallback):
        """Custom SB3 callback that logs to our lightweight TensorBoard server."""

        def __init__(self, run_id: str, verbose: int = 0):
            super().__init__(verbose)
            self.run_id = run_id
            self.step_count = 0

        def _on_step(self) -> bool:
            """Called after each step."""
            self.step_count += 1
            if self.n_calls % 10 == 0:
                # Log episode reward mean
                if len(self.model.ep_info_buffer) > 0:
                    mean_reward = np.mean([ep["r"] for ep in self.model.ep_info_buffer])
                    get_server().add_scalar(self.run_id, "train/mean_reward", self.n_calls, mean_reward)
                # Log value loss if available
                if hasattr(self.model, "logger") and self.model.logger is not None:
                    for key, value in self.model.logger.name_to_value.items():
                        if isinstance(value, (int, float)):
                            get_server().add_scalar(self.run_id, f"train/{key}", self.n_calls, float(value))
            return True

except ImportError:
    HAS_SB3 = False
    # Placeholder for when SB3 is not installed
    class TensorBoardCallback:
        def __init__(self, *args, **kwargs):
            pass


class QLearningTrainer:
    """Simple tabular Q-learning for the trading environment."""

    def __init__(self, config: Optional[Dict] = None):
        self.config = config or {}
        self.learning_rate = self.config.get("learning_rate", 0.01)
        self.discount = self.config.get("discount", 0.99)
        self.epsilon = self.config.get("epsilon", 0.1)
        self.epsilon_decay = self.config.get("epsilon_decay", 0.995)
        self.episodes = self.config.get("episodes", 100)
        self.bins = self.config.get("bins", 10)
        self.q_table: Dict[int, np.ndarray] = {}
        self.run_id = self.config.get("run_id", "ql_" + str(int(time.time())))

    def _discretize(self, obs: np.ndarray) -> int:
        key_indices = [0, 1, 2, -2, -1]
        indices = [i for i in key_indices if i < len(obs)]
        key_vals = obs[indices]
        bins = np.linspace(-1, 1, self.bins)
        discretized = np.digitize(key_vals.clip(-1, 1), bins)
        return hash(tuple(discretized))

    def train(self, env: Any, features: np.ndarray, prices: np.ndarray) -> Dict[str, Any]:
        env.set_data(features, prices)
        n_actions = env.action_space if isinstance(env.action_space, int) else 5

        history = {"episode_rewards": [], "final_balances": [], "episode_lengths": []}

        # Register run with TensorBoard
        get_server().create_run(self.run_id, self.run_id, "rl", self.run_id)

        for episode in range(self.episodes):
            obs = env.reset()
            total_reward = 0
            done = False
            steps = 0

            while not done and steps < len(prices) - env.window_size - 1:
                state = self._discretize(obs)

                if np.random.random() < self.epsilon:
                    action = np.random.randint(n_actions)
                else:
                    if state not in self.q_table:
                        self.q_table[state] = np.zeros(n_actions)
                    action = int(np.argmax(self.q_table[state]))

                obs_next, reward, terminated, truncated, info = env.step(action)
                done = terminated or truncated

                state_next = self._discretize(obs_next)
                if state not in self.q_table:
                    self.q_table[state] = np.zeros(n_actions)
                if state_next not in self.q_table:
                    self.q_table[state_next] = np.zeros(n_actions)

                best_next = np.max(self.q_table[state_next])
                self.q_table[state][action] += self.learning_rate * (
                    reward + self.discount * best_next - self.q_table[state][action]
                )

                obs = obs_next
                total_reward += reward
                steps += 1

            self.epsilon *= self.epsilon_decay
            history["episode_rewards"].append(float(total_reward))
            history["final_balances"].append(float(env.balance))
            history["episode_lengths"].append(steps)

            # Log to TensorBoard every episode
            get_server().add_scalar(self.run_id, "train/episode_reward", episode, float(total_reward))
            get_server().add_scalar(self.run_id, "train/final_balance", episode, float(env.balance))
            get_server().add_scalar(self.run_id, "train/epsilon", episode, float(self.epsilon))
            get_server().add_scalar(self.run_id, "train/q_table_size", episode, len(self.q_table))

            if (episode + 1) % max(1, self.episodes // 10) == 0:
                print(f"  Episode {episode + 1}/{self.episodes}: "
                      f"reward={total_reward:.2f}, balance={env.balance:.2f}, "
                      f"ε={self.epsilon:.4f}, q_size={len(self.q_table)}")

        get_server().finish_run(self.run_id, "completed")

        return {
            "episodes": self.episodes,
            "final_balance": float(env.balance),
            "total_pnl": float(env.total_pnl),
            "best_reward": float(max(history["episode_rewards"])),
            "avg_reward_last_10": float(np.mean(history["episode_rewards"][-10:])),
            "episode_rewards": history["episode_rewards"],
            "q_table_size": len(self.q_table),
        }

    def get_best_action(self, obs: np.ndarray, n_actions: int) -> int:
        state = self._discretize(obs)
        if state in self.q_table:
            return int(np.argmax(self.q_table[state]))
        return 1  # default: NEUTRAL


class SB3Trainer:
    """Stable-Baselines3 trainer (PPO/A2C/SAC) with TensorBoard logging."""

    def __init__(self, config: Optional[Dict] = None):
        self.config = config or {}
        self.algorithm = self.config.get("algorithm", "ppo")
        self.n_actions = self.config.get("n_actions", 3)
        self.episodes = self.config.get("episodes", 100)
        self.total_timesteps = self.config.get("total_timesteps", 10000)
        self.run_id = self.config.get("run_id", f"sb3_{self.algorithm}_" + str(int(time.time())))
        self.model = None

    def train(self, bars: List[Dict], features: Optional[np.ndarray] = None) -> Dict[str, Any]:
        if not HAS_SB3:
            return {"error": "stable-baselines3 not installed", "success": False}

        prices = np.array([b["close"] for b in bars], dtype=np.float64)

        if features is None:
            n = len(prices)
            features = np.zeros((n, 5), dtype=np.float64)
            features[1:, 0] = np.diff(prices) / (prices[:-1] + 1e-8)
            features[1:, 1] = np.log(prices[1:] / (prices[:-1] + 1e-8))
            for p in [5, 10, 20]:
                if n > p:
                    ma = np.convolve(prices, np.ones(p)/p, mode='same')
                    idx = min(2 + [5, 10, 20].index(p), 4)
                    features[:, idx] = (prices - ma) / (ma + 1e-8)

        # Create environment via gymnasium.make for SB3 compatibility
        import gymnasium as gym
        from gymnasium.wrappers import OrderEnforcing, PassiveEnvChecker
        env_config = {
            "window_size": self.config.get("window_size", 50),
            "initial_balance": self.config.get("initial_balance", 10000),
            "commission": self.config.get("commission", 0.001),
        }
        
        # Directly instantiate and wrap like gymnasium.make does
        if self.n_actions == 5:
            raw_env = gym.envs.registration.load_env_creator("models.rl_env:TradingEnv5Action")(config=env_config)
        else:
            from models.rl_env import TradingEnv3Action
            raw_env = TradingEnv3Action(config=env_config)
        
        # Apply gymnasium wrappers manually to ensure proper type chain
        env = OrderEnforcing(PassiveEnvChecker(raw_env))
        env.unwrapped.set_data(features, prices)

        # Register run
        get_server().create_run(self.run_id, self.run_id, "rl", self.run_id)

        # Create model
        algo_class = {"ppo": PPO, "a2c": A2C, "sac": SAC}.get(self.algorithm.lower(), PPO)
        self.model = algo_class("MlpPolicy", env, verbose=0)

        # Train with custom callback
        callback = TensorBoardCallback(self.run_id)
        self.model.learn(total_timesteps=self.total_timesteps, callback=callback)

        get_server().finish_run(self.run_id, "completed")

        return {
            "success": True,
            "algorithm": self.algorithm,
            "n_actions": self.n_actions,
            "total_timesteps": self.total_timesteps,
            "model_path": f"./models/{self.run_id}.zip",
        }

    def predict(self, obs: np.ndarray) -> Tuple[int, float]:
        if self.model is None:
            return 1, 0.0
        action, _states = self.model.predict(obs, deterministic=True)
        return int(action), 1.0


class RLTrainer:
    """Unified RL trainer supporting both Q-learning and SB3."""

    def __init__(self, config: Optional[Dict] = None):
        self.config = config or {}
        self.algorithm = self.config.get("algorithm", "qlearning")
        self.n_actions = self.config.get("n_actions", 3)
        self.episodes = self.config.get("episodes", 100)
        self.model = None
        self.trainer = None

    def train(self, bars: List[Dict], features: Optional[np.ndarray] = None) -> Dict[str, Any]:
        prices = np.array([b["close"] for b in bars], dtype=np.float64)

        if features is None:
            n = len(prices)
            features = np.zeros((n, 5), dtype=np.float64)
            features[1:, 0] = np.diff(prices) / (prices[:-1] + 1e-8)
            features[1:, 1] = np.log(prices[1:] / (prices[:-1] + 1e-8))
            for p in [5, 10, 20]:
                if n > p:
                    ma = np.convolve(prices, np.ones(p)/p, mode='same')
                    idx = min(2 + [5, 10, 20].index(p), 4)
                    features[:, idx] = (prices - ma) / (ma + 1e-8)

        # Use SB3 for advanced algorithms
        if self.algorithm in ["ppo", "a2c", "sac"]:
            if not HAS_SB3:
                return {
                    "success": False,
                    "error": f"Algorithm '{self.algorithm}' requires stable-baselines3. Install with: pip install stable-baselines3",
                }
            self.trainer = SB3Trainer(self.config)
            return self.trainer.train(bars, features)

        # Default: Q-learning
        env_config = {
            "window_size": self.config.get("window_size", 50),
            "initial_balance": self.config.get("initial_balance", 10000),
            "commission": self.config.get("commission", 0.001),
        }
        if self.n_actions == 5:
            env = TradingEnv5Action(env_config)
        else:
            env = TradingEnv3Action(env_config)

        trainer = QLearningTrainer(self.config)
        result = trainer.train(env, features, prices)
        result["algorithm"] = self.algorithm
        result["n_actions"] = self.n_actions
        self.trainer = trainer
        return result

    def predict(self, bars: List[Dict], features: Optional[np.ndarray] = None) -> Dict[str, Any]:
        if self.trainer is None:
            return {"success": False, "error": "Model not trained"}

        prices = np.array([b["close"] for b in bars], dtype=np.float64)
        if features is None:
            n = len(prices)
            features = np.zeros((n, 5), dtype=np.float64)
            features[1:, 0] = np.diff(prices) / (prices[:-1] + 1e-8)
            features[1:, 1] = np.log(prices[1:] / (prices[:-1] + 1e-8))
            for p in [5, 10, 20]:
                if n > p:
                    ma = np.convolve(prices, np.ones(p)/p, mode='same')
                    idx = min(2 + [5, 10, 20].index(p), 4)
                    features[:, idx] = (prices - ma) / (ma + 1e-8)

        env_config = {
            "window_size": self.config.get("window_size", 50),
            "initial_balance": self.config.get("initial_balance", 10000),
            "commission": self.config.get("commission", 0.001),
        }
        if self.n_actions == 5:
            env = TradingEnv5Action(env_config)
        else:
            env = TradingEnv3Action(env_config)
        env.set_data(features, prices)

        obs = env.reset()
        if isinstance(self.trainer, SB3Trainer):
            action, confidence = self.trainer.predict(obs)
        else:
            action = self.trainer.get_best_action(obs, self.n_actions)
            confidence = 1.0 if self.trainer.q_table else 0.0

        action_names = {
            3: {0: "SHORT", 1: "NEUTRAL", 2: "LONG"},
            5: {0: "FULL_SHORT", 1: "HALF_SHORT", 2: "NEUTRAL", 3: "HALF_LONG", 4: "FULL_LONG"},
        }
        multipliers = {3: [-1.0, 0.0, 1.0], 5: [-1.0, -0.5, 0.0, 0.5, 1.0]}

        return {
            "success": True,
            "action": int(action),
            "action_name": action_names.get(self.n_actions, {}).get(action, "UNKNOWN"),
            "confidence": float(confidence),
            "position": multipliers.get(self.n_actions, [0, 0, 0])[action] if action < len(multipliers.get(self.n_actions, [])) else 0,
        }

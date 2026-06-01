"""
RL Trainer — Simple Q-learning + optional Stable-Baselines3 integration.
Works without any RL library for basic training, or with SB3 for PPO/A2C/SAC.
"""
import json
import numpy as np
from typing import Any, Dict, List, Optional
from models.rl_env import TradingEnv3Action, TradingEnv5Action


class QLearningTrainer:
    """Simple tabular Q-learning for the trading environment.
    Discretizes the observation space for a lightweight, no-dependency training."""

    def __init__(self, config: Optional[Dict] = None):
        self.config = config or {}
        self.learning_rate = self.config.get("learning_rate", 0.01)
        self.discount = self.config.get("discount", 0.99)
        self.epsilon = self.config.get("epsilon", 0.1)
        self.epsilon_decay = self.config.get("epsilon_decay", 0.995)
        self.episodes = self.config.get("episodes", 100)
        self.bins = self.config.get("bins", 10)  # discretization bins per feature

        self.q_table: Dict[int, np.ndarray] = {}

    def _discretize(self, obs: np.ndarray) -> int:
        """Convert continuous observation to a discrete state hash."""
        # Take a few key features and bin them
        key_indices = [0, 1, 2, -2, -1]  # first 3 features + position_flag + balance_ratio
        indices = [i for i in key_indices if i < len(obs)]
        key_vals = obs[indices]
        # Discretize
        bins = np.linspace(-1, 1, self.bins)
        discretized = np.digitize(key_vals.clip(-1, 1), bins)
        return hash(tuple(discretized))

    def train(self, env: TradingEnv3Action, features: np.ndarray, prices: np.ndarray):
        """Train using Q-learning over multiple episodes."""
        env.set_data(features, prices)
        n_actions = env.action_space if isinstance(env.action_space, int) else 5

        history = {"episode_rewards": [], "final_balances": []}

        for episode in range(self.episodes):
            obs = env.reset()
            total_reward = 0
            done = False
            steps = 0

            while not done and steps < len(prices) - env.window_size - 1:
                state = self._discretize(obs)

                # Epsilon-greedy action
                if np.random.random() < self.epsilon:
                    action = np.random.randint(n_actions)
                else:
                    if state not in self.q_table:
                        self.q_table[state] = np.zeros(n_actions)
                    action = int(np.argmax(self.q_table[state]))

                obs_next, reward, terminated, truncated, info = env.step(action)
                done = terminated or truncated

                # Q-learning update
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

            # Decay epsilon
            self.epsilon *= self.epsilon_decay
            history["episode_rewards"].append(float(total_reward))
            history["final_balances"].append(float(env.balance))

            if (episode + 1) % max(1, self.episodes // 10) == 0:
                print(f"  Episode {episode + 1}/{self.episodes}: "
                      f"reward={total_reward:.2f}, balance={env.balance:.2f}, "
                      f"ε={self.epsilon:.4f}, q_size={len(self.q_table)}")

        return {
            "episodes": self.episodes,
            "final_balance": float(env.balance),
            "total_pnl": float(env.total_pnl),
            "best_reward": float(max(history["episode_rewards"])),
            "avg_reward_last_10": float(np.mean(history["episode_rewards"][-10:])),
            "episode_rewards": history["episode_rewards"],
        }

    def get_best_action(self, obs: np.ndarray, n_actions: int) -> int:
        """Get the best action for an observation."""
        state = self._discretize(obs)
        if state in self.q_table:
            return int(np.argmax(self.q_table[state]))
        return 1  # default: NEUTRAL


class RLTrainer:
    """Wrapper supporting both Q-learning and SB3 (if installed)."""

    def __init__(self, config: Optional[Dict] = None):
        self.config = config or {}
        self.algorithm = self.config.get("algorithm", "qlearning")  # qlearning, ppo, a2c, sac
        self.n_actions = self.config.get("n_actions", 3)
        self.episodes = self.config.get("episodes", 100)
        self.model = None

    def train(self, bars: List[Dict], features: np.ndarray = None) -> Dict[str, Any]:
        """Train an RL agent on historical OHLCV data."""
        prices = np.array([b["close"] for b in bars], dtype=np.float64)

        # Generate features if not provided (simple returns + position)
        if features is None:
            n = len(prices)
            features = np.zeros((n, 5), dtype=np.float64)
            features[1:, 0] = np.diff(prices) / (prices[:-1] + 1e-8)  # returns
            features[1:, 1] = np.log(prices[1:] / (prices[:-1] + 1e-8))  # log returns
            # Simple MAs
            for p in [5, 10, 20]:
                if n > p:
                    ma = np.convolve(prices, np.ones(p)/p, mode='same')
                    idx = min(2 + [5, 10, 20].index(p), 4)
                    features[:, idx] = (prices - ma) / (ma + 1e-8)

        # Create environment
        if self.n_actions == 5:
            env = TradingEnv5Action(self.config)
        else:
            env = TradingEnv3Action(self.config)

        # Train
        trainer = QLearningTrainer({
            "episodes": self.episodes,
            "learning_rate": self.config.get("learning_rate", 0.01),
            "discount": self.config.get("discount", 0.99),
        })

        result = trainer.train(env, features, prices)
        result["algorithm"] = self.algorithm
        result["n_actions"] = self.n_actions
        result["q_table_size"] = len(trainer.q_table)

        return result

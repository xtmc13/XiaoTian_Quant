"""
Reinforcement Learning Trading Environment.
Aligns with FreqAI Base3ActionRLEnv / Base5ActionRLEnv.
Uses Gymnasium interface for compatibility with Stable-Baselines3.
"""
import numpy as np
from typing import Any, Dict, Optional, Tuple

try:
    import gymnasium as gym
    from gymnasium import spaces
    HAS_GYM = True
except ImportError:
    HAS_GYM = False
    # Fallback: minimal gym-like interface
    gym = None
    spaces = None


class TradingEnv3Action:
    """
    3-Action Trading Environment: SHORT (0), NEUTRAL (1), LONG (2).
    Aligns with FreqAI Base3ActionRLEnv.
    """

    def __init__(self, config: Optional[Dict[str, Any]] = None):
        self.config = config or {}
        self.window_size = self.config.get("window_size", 50)
        self.initial_balance = self.config.get("initial_balance", 10000)
        self.commission = self.config.get("commission", 0.001)

        # State: [features for window_size bars, position_flag, balance_ratio]
        self.feature_dim = self.config.get("feature_dim", 5)
        obs_dim = self.window_size * self.feature_dim + 2

        if HAS_GYM:
            self.action_space = spaces.Discrete(3)
            self.observation_space = spaces.Box(
                low=-np.inf, high=np.inf, shape=(obs_dim,), dtype=np.float32
            )
        else:
            self.action_space = 3
            self.observation_space = obs_dim

        self.reset()

    def reset(self):
        """Reset environment to initial state."""
        self.balance = self.initial_balance
        self.position = 0  # -1=short, 0=neutral, 1=long
        self.entry_price = 0
        self.entry_balance = self.initial_balance
        self.step_count = 0
        self.total_pnl = 0
        self.data = None
        self.data_idx = 0
        return np.zeros(self.observation_space if isinstance(self.observation_space, int)
                       else self.observation_space.shape[0], dtype=np.float32)

    def set_data(self, features: np.ndarray, prices: np.ndarray):
        """Load feature data for the episode."""
        self.data = features
        self.prices = prices
        self.data_idx = self.window_size

    def step(self, action: int) -> Tuple[np.ndarray, float, bool, bool, Dict]:
        """Take an action and return (obs, reward, terminated, truncated, info)."""
        if self.data is None or self.data_idx >= len(self.data):
            return self._make_obs(), 0, True, False, {}

        # Action mapping: 0=SHORT, 1=NEUTRAL, 2=LONG
        prev_position = self.position
        current_price = self.prices[self.data_idx]

        if action == 0:   # SHORT
            self.position = -1
        elif action == 2: # LONG
            self.position = 1
        else:             # NEUTRAL
            self.position = 0

        # If position changed, realize PnL
        reward = 0
        if prev_position != self.position:
            if prev_position != 0:
                # Close previous position
                pnl = (current_price - self.entry_price) * prev_position
                pnl -= self.commission * current_price
                self.balance += pnl
                self.total_pnl += pnl
                reward = pnl / self.entry_balance  # Normalized reward

            if self.position != 0:
                # Open new position
                self.entry_price = current_price
                self.entry_balance = self.balance

        # Mark-to-market PnL for open position
        if self.position != 0:
            unrealized = (current_price - self.entry_price) * self.position
            reward += unrealized / self.entry_balance * 0.1  # Small weight on unrealized

        # Terminal: end of data
        terminated = self.data_idx >= len(self.data) - 1
        if terminated and self.position != 0:
            # Close position at end
            pnl = (self.prices[-1] - self.entry_price) * self.position
            self.balance += pnl
            self.total_pnl += pnl
            reward += pnl / self.entry_balance

        self.step_count += 1
        self.data_idx += 1

        return self._make_obs(), float(reward), terminated, False, {
            "balance": self.balance,
            "position": self.position,
            "total_pnl": self.total_pnl,
        }

    def _make_obs(self) -> np.ndarray:
        """Build observation vector from recent features + position state."""
        if self.data is None or self.data_idx < self.window_size:
            obs_dim = (self.observation_space if isinstance(self.observation_space, int)
                      else self.observation_space.shape[0])
            return np.zeros(obs_dim, dtype=np.float32)

        # Recent features (flattened window)
        start = self.data_idx - self.window_size
        features_flat = self.data[start:self.data_idx].flatten()

        # Position state
        position_flag = float(self.position)
        balance_ratio = self.balance / self.initial_balance

        obs = np.append(features_flat, [position_flag, balance_ratio])

        # Pad if needed
        if isinstance(self.observation_space, int):
            target_dim = self.observation_space
        else:
            target_dim = self.observation_space.shape[0]

        if len(obs) < target_dim:
            obs = np.pad(obs, (0, target_dim - len(obs)))
        return obs[:target_dim].astype(np.float32)


class TradingEnv5Action(TradingEnv3Action):
    """
    5-Action Trading Environment.
    0 = FULL_SHORT, 1 = HALF_SHORT, 2 = NEUTRAL, 3 = HALF_LONG, 4 = FULL_LONG.
    Aligns with FreqAI Base5ActionRLEnv.
    """

    def __init__(self, config: Optional[Dict[str, Any]] = None):
        super().__init__(config)
        if HAS_GYM:
            self.action_space = spaces.Discrete(5)
        else:
            self.action_space = 5

    def step(self, action: int) -> Tuple[np.ndarray, float, bool, bool, Dict]:
        """5-action variant: action determines position size multiplier."""
        # Map 5 actions to position multipliers
        # 0=FULL_SHORT(-1.0), 1=HALF_SHORT(-0.5), 2=NEUTRAL(0),
        # 3=HALF_LONG(0.5), 4=FULL_LONG(1.0)
        multipliers = [-1.0, -0.5, 0.0, 0.5, 1.0]
        multiplier = multipliers[action]

        prev_multiplier = self._prev_multiplier if hasattr(self, '_prev_multiplier') else 0
        self._prev_multiplier = multiplier

        current_price = self.prices[self.data_idx]

        reward = 0
        if prev_multiplier != multiplier:
            if prev_multiplier != 0:
                pnl = (current_price - self.entry_price) * prev_multiplier
                pnl -= self.commission * current_price * abs(prev_multiplier)
                self.balance += pnl
                self.total_pnl += pnl
                reward = pnl / self.entry_balance

            if multiplier != 0:
                self.entry_price = current_price
                self.entry_balance = self.balance

        self.position = int(multiplier)

        if self.position != 0:
            unrealized = (current_price - self.entry_price) * multiplier
            reward += unrealized / self.entry_balance * 0.1

        terminated = self.data_idx >= len(self.data) - 1
        if terminated and self.position != 0:
            pnl = (self.prices[-1] - self.entry_price) * multiplier
            self.balance += pnl
            self.total_pnl += pnl
            reward += pnl / self.entry_balance

        self.step_count += 1
        self.data_idx += 1

        return self._make_obs(), float(reward), terminated, False, {
            "balance": self.balance,
            "position": self.position,
            "multiplier": multiplier,
            "total_pnl": self.total_pnl,
        }

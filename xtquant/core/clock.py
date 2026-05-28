"""
统一时钟 — 实盘用系统时间，回测用模拟时间
所有需要时间的地方都通过Clock获取，保证回测一致性
"""

import time


class Clock:
    """
    统一时钟

    设计原则:
      - 实盘模式: 返回真实系统时间
      - 回测模式: 返回模拟时间（由回测引擎推动）
      - 所有代码通过 clock.time() / clock.time_ms() 获取时间
      - 绝不直接调用 time.time()

    Usage:
      clock = Clock()
      now_ms = clock.time_ms()   # 毫秒时间戳
      now_s = clock.time()       # 秒级时间戳
      clock.advance(new_time)    # 回测引擎推动时间前进
    """

    def __init__(self, mode: str = "live"):
        self.mode = mode  # live | backtest
        self._sim_time_ns: int = 0
        self._start_real_ns: int = time.time_ns()

    def time(self) -> float:
        """当前Unix秒级时间戳"""
        if self.mode == "backtest":
            return self._sim_time_ns / 1_000_000_000
        return time.time()

    def time_ms(self) -> int:
        """当前Unix毫秒级时间戳"""
        if self.mode == "backtest":
            return self._sim_time_ns // 1_000_000
        return int(time.time() * 1000)

    def time_ns(self) -> int:
        """当前Unix纳秒级时间戳"""
        if self.mode == "backtest":
            return self._sim_time_ns
        return time.time_ns()

    def monotonic(self) -> float:
        """单调时钟（用于计算间隔，不受系统时间调整影响）"""
        return time.monotonic()

    def advance(self, timestamp_ms: int):
        """
        回测模式：推进模拟时间

        Args:
            timestamp_ms: 下一个事件的时间戳（毫秒）
        """
        self._sim_time_ns = timestamp_ms * 1_000_000

    def advance_s(self, timestamp_s: float):
        """推进模拟时间（秒）"""
        self._sim_time_ns = int(timestamp_s * 1_000_000_000)

    def reset(self):
        """重置时钟"""
        self._sim_time_ns = 0
        self._start_real_ns = time.time_ns()

    @staticmethod
    def real_time() -> float:
        """始终返回真实时间（即使用于日志等）"""
        return time.time()

    @staticmethod
    def real_time_ms() -> int:
        """始终返回真实时间毫秒"""
        return int(time.time() * 1000)

    def __repr__(self):
        return f"Clock(mode={self.mode}, time={self.time_ms()})"

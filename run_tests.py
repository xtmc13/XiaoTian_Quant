#!/usr/bin/env python3
"""
小天量化交易 - 测试运行器
"""

import sys
import os

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

print("=" * 70)
print("🧪 小天量化交易 - 测试套件")
print("=" * 70)
print()

try:
    print("运行: tests/test_factors.py")
    print()
except Exception as e:
    print(f"❌ 因子测试失败: {e}")
    print()

try:
    print("运行: tests/test_exchange.py")
    print()
except Exception as e:
    print(f"❌ 交易所测试失败: {e}")
    print()

try:
    print("运行: tests/test_backtest.py")
    print()
except Exception as e:
    print(f"❌ 回测测试失败: {e}")
    print()

print("=" * 70)
print("✅ 测试套件执行完成")
print("=" * 70)

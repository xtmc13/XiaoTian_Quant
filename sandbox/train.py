#!/usr/bin/env python3
"""XiaoTianQuant Model Training CLI"""
import sys
import os

sys.path.insert(0, os.path.dirname(__file__))
from ml_server.main import main

if __name__ == "__main__":
    main()

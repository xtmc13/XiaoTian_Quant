FROM python:3.11-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    g++ \
    make \
    autoconf \
    automake \
    libtool \
    libc6-dev \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install TA-Lib C library from GitHub (more reliable than SourceForge)
RUN curl -fsSL https://github.com/TA-Lib/ta-lib/releases/download/v0.4.0/ta-lib-0.4.0-src.tar.gz \
      -o /tmp/ta-lib.tar.gz \
    && cd /tmp && tar xzf ta-lib.tar.gz \
    && cd ta-lib \
    && chmod +x configure autogen.sh 2>/dev/null || true \
    && ./configure --prefix=/usr \
    && make \
    && make install \
    && cd / && rm -rf /tmp/ta-lib*

# Install Python dependencies (before copying code, for layer caching)
COPY requirements.txt .
RUN pip install --no-cache-dir "TA-Lib>=0.6.5" \
    && pip install --no-cache-dir fastapi==0.115.0 "uvicorn[standard]==0.32.0" pandas==2.2.3 pydantic==2.9.2 python-multipart==0.0.17

# Copy all sandbox code
COPY . .

# Install additional deps for ml_server, ccxt_bridge and strategy_engine
RUN if [ -f ccxt_bridge/requirements.txt ]; then \
        pip install --no-cache-dir -r ccxt_bridge/requirements.txt || true; \
    fi; \
    if [ -f ml_server/requirements.txt ]; then \
        pip install --no-cache-dir -r ml_server/requirements.txt || true; \
    fi; \
    if [ -f strategy_engine/requirements.txt ]; then \
        pip install --no-cache-dir -r strategy_engine/requirements.txt || true; \
    fi

# Run as non-root
RUN useradd -m -u 1000 sandboxuser && chown -R sandboxuser:sandboxuser /app
USER sandboxuser

ENV SANDBOX_PORT=9000
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

EXPOSE 9000 8001 8002

# 沙箱内全部四个服务随容器启动：
#   main.py          :9000  指标/策略代码执行器（SANDBOX_URL）
#   ml_server.server :8001  ML/RL/TensorBoard（ML_SERVER_URL）
#   ccxt_bridge.main :8002  CCXT 交易所桥（CCXT_BRIDGE_URL）
#   strategy_engine  :8003  Python 策略引擎（PYTHON_STRATEGY_URL）
CMD ["sh", "-c", "uvicorn main:app --host 0.0.0.0 --port 9000 --log-level info & uvicorn ml_server.server:app --host 0.0.0.0 --port 8001 --log-level info & uvicorn ccxt_bridge.main:app --host 0.0.0.0 --port 8002 --log-level info & uvicorn strategy_engine.main:app --host 0.0.0.0 --port 8003 --log-level info & wait"]

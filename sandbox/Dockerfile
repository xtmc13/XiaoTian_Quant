FROM python:3.11-slim

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    g++ \
    make \
    libc6-dev \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install TA-Lib C library (required by TA-Lib Python package)
RUN curl -fsSL http://prdownloads.sourceforge.net/ta-lib/ta-lib-0.4.0-src.tar.gz -o /tmp/ta-lib.tar.gz \
    && cd /tmp && tar xzf ta-lib.tar.gz \
    && cd ta-lib \
    && ./configure --prefix=/usr \
    && make \
    && make install \
    && cd / && rm -rf /tmp/ta-lib*

# Install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Install additional deps for ml_server and ccxt_bridge
RUN if [ -f ccxt_bridge/requirements.txt ]; then \
        cp ccxt_bridge/requirements.txt /tmp/ccxt_req.txt && \
        pip install --no-cache-dir -r /tmp/ccxt_req.txt && \
        rm /tmp/ccxt_req.txt; \
    fi; \
    if [ -f ml_server/requirements.txt ]; then \
        cp ml_server/requirements.txt /tmp/ml_req.txt && \
        pip install --no-cache-dir -r /tmp/ml_req.txt && \
        rm /tmp/ml_req.txt; \
    fi

# Copy all sandbox code (main, executor, analyzer, indicators, ccxt_bridge, ml_server)
COPY . .

# Run as non-root user
RUN useradd -m -u 1000 sandboxuser && chown -R sandboxuser:sandboxuser /app
USER sandboxuser

ENV SANDBOX_PORT=9000
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

EXPOSE 9000 8001 8002

# Default: run sandbox service; override with command for ml_server / ccxt_bridge
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "9000", "--log-level", "info"]

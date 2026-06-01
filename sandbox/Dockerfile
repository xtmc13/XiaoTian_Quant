FROM python:3.11-slim

WORKDIR /app

# Install system dependencies for numpy/pandas/ta-lib
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    g++ \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy sandbox code
COPY main.py executor.py analyzer.py ./

# Run as non-root user
RUN useradd -m -u 1000 sandboxuser
USER sandboxuser

ENV SANDBOX_PORT=9000
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

EXPOSE 9000

CMD ["python", "-m", "uvicorn", "main:app", "--host", "0.0.0.0", "--port", "9000", "--log-level", "info"]

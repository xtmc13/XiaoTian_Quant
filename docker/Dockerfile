FROM python:3.12-slim-bookworm

LABEL org.opencontainers.image.title="XiaoTianQuant Backend"
LABEL org.opencontainers.image.description="AI-powered quantitative trading system"

ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1
ENV TZ=Asia/Shanghai

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates gcc g++ \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

RUN mkdir -p /app/logs /app/data

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
    CMD curl -f http://localhost:8080/api/status || exit 1

CMD ["python", "run_web.py"]

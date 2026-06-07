@echo off
chcp 65001 >nul
title XiaoTianQuant Docker 一键安装器
color 0A
cls

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║     XiaoTianQuant 多系统 Docker 一键安装器                  ║
echo  ║     自动构建 + 启动所有服务（Gateway/ML/Redis）            ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.

:: 检查 Docker
echo [1/5] 检查 Docker 环境...
docker --version >nul 2>&1
if %errorlevel% neq 0 (
    echo  ❌ Docker 未安装或未启动
    echo  请先安装 Docker Desktop: https://www.docker.com/products/docker-desktop/
    pause
    exit /b 1
)
echo  ✅ Docker 已安装

:: 检查 Docker Compose
echo [2/5] 检查 Docker Compose...
docker compose version >nul 2>&1
if %errorlevel% neq 0 (
    echo  ❌ Docker Compose 不可用
    pause
    exit /b 1
)
echo  ✅ Docker Compose 可用

:: 设置代理环境变量（构建时使用）
echo [3/5] 配置网络代理...
set HTTP_PROXY=http://host.docker.internal:7897
set HTTPS_PROXY=http://host.docker.internal:7897
set NO_PROXY=localhost,127.0.0.1

:: 创建 .env 文件（如果不存在）
if not exist .env (
    echo  创建默认配置 .env 文件...
    (
        echo # XiaoTianQuant 环境配置
        echo HTTP_PROXY=http://host.docker.internal:7897
        echo HTTPS_PROXY=http://host.docker.internal:7897
        echo VERSION=3.0.0
        echo LOG_LEVEL=INFO
        echo.
        echo # 可选：交易所 API 密钥
        echo BINANCE_API_KEY=
        echo BINANCE_API_SECRET=
        echo.
        echo # 可选：AI 提供商密钥
        echo DEEPSEEK_API_KEY=
        echo OPENAI_API_KEY=
    ) > .env
)

:: 构建并启动
echo [4/5] 构建并启动多系统服务...
echo  这可能需要 5-10 分钟（首次构建）...
cd /d C:\Users\20545\Desktop\xiaotian_quant

docker compose up --build -d

if %errorlevel% neq 0 (
    echo  ❌ 启动失败
    echo  尝试不使用缓存重新构建...
    docker compose build --no-cache
    docker compose up -d
)

:: 等待服务就绪
echo [5/5] 等待服务就绪...
timeout /t 10 /nobreak >nul

echo.
echo  检查服务状态...
docker compose ps

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║  ✅ 多系统安装完成！                                      ║
echo  ║                                                           ║
echo  ║  访问地址: http://localhost:8080                         ║
echo  ║  登录账号: admin / admin123                                ║
echo  ║                                                           ║
echo  ║  服务组成:                                                ║
echo  ║    • Gateway    :8080  (Go 网关 + 前端)                   ║
echo  ║    • ML Server  :8001  (Python 模型服务)                  ║
echo  ║    • Sandbox    :9000  (Python 沙箱)                     ║
echo  ║    • CCXT Bridge:8002  (交易所桥接)                      ║
echo  ║    • Redis      :6379  (缓存/队列)                       ║
echo  ║                                                           ║
echo  ║  管理命令:                                                ║
echo  ║    docker compose logs -f    查看日志                   ║
echo  ║    docker compose stop       停止服务                     ║
echo  ║    docker compose down       删除容器                     ║
echo  ║    docker compose pull       更新镜像                     ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.
pause

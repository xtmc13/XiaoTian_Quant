@echo off
chcp 65001 >nul
title XiaoTianQuant 多系统一键启动器
color 0A
cls

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║     XiaoTianQuant 多系统一键启动器                        ║
echo  ║     本地模式（无需 Docker）                                ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.

:: 设置项目路径
set PROJECT_DIR=C:\Users\20545\Desktop\xiaotian_quant
set PYTHON=C:\Users\20545\AppData\Local\Programs\Python\Python312\python.exe
set NODE=node.exe

:: 检查依赖
echo [1/4] 检查运行环境...

if not exist "%PYTHON%" (
    echo  ❌ Python 未找到: %PYTHON%
    echo  请安装 Python 3.12: https://www.python.org/downloads/
    pause
    exit /b 1
)
echo  ✅ Python: %PYTHON%

%PYTHON% -c "import fastapi" >nul 2>&1
if %errorlevel% neq 0 (
    echo  📦 安装 Python 依赖...
    %PYTHON% -m pip install -r "%PROJECT_DIR%\sandbox\ml_server\requirements.txt"
)

:: 检查数据库数据
echo [2/4] 检查市场数据...
%PYTHON% -c "import sqlite3; conn=sqlite3.connect(r'%PROJECT_DIR%\gateway\gateway.db'); c=conn.cursor(); c.execute('SELECT COUNT(*) FROM market_bars'); count=c.fetchone()[0]; print('  数据库已有', count, '条 K线数据'); conn.close()"

:: 启动 ML Server
echo [3/4] 启动 ML Server (Python)...
start "ML Server :8001" cmd /c "cd /d %PROJECT_DIR%\sandbox\ml_server && %PYTHON% -m uvicorn main:app --host 0.0.0.0 --port 8001"
timeout /t 3 /nobreak >nul

:: 启动 Go Gateway
echo [4/4] 启动 Go Gateway...
if exist "%PROJECT_DIR%\gateway\gateway-server.exe" (
    start "Gateway :8080" cmd /c "cd /d %PROJECT_DIR%\gateway && gateway-server.exe"
) else (
    echo  ⚠️ gateway-server.exe 不存在，前端将无法通过 8080 访问
    echo  请使用 5173 端口（Vite 开发服务器）
)
timeout /t 3 /nobreak >nul

:: 启动前端开发服务器
echo [5/5] 启动前端开发服务器...
start "Vite :5173" cmd /c "cd /d %PROJECT_DIR%\web && %NODE% node_modules\vite\bin\vite.js --port 5173"
timeout /t 5 /nobreak >nul

:: 检查端口
echo.
echo  检查服务端口...
netstat -ano | findstr ":8080" | findstr "LISTENING" >nul && echo  ✅ Gateway    :8080  运行中 || echo  ⚠️ Gateway    :8080  未启动
netstat -ano | findstr ":8001" | findstr "LISTENING" >nul && echo  ✅ ML Server  :8001  运行中 || echo  ⚠️ ML Server  :8001  未启动
netstat -ano | findstr ":5173" | findstr "LISTENING" >nul && echo  ✅ Vite       :5173  运行中 || echo  ⚠️ Vite       :5173  未启动

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║  ✅ 多系统启动完成！                                      ║
echo  ║                                                           ║
echo  ║  访问地址（推荐）: http://localhost:5173                   ║
echo  ║  备用地址        : http://localhost:8080                   ║
echo  ║  登录账号        : admin / admin123                        ║
echo  ║                                                           ║
echo  ║  多系统组成:                                              ║
echo  ║    • Go Gateway    :8080  API网关 + 策略引擎              ║
echo  ║    • ML Server     :8001  机器学习服务                    ║
echo  ║    • Vite Dev      :5173  前端开发服务器                  ║
echo  ║                                                           ║
echo  ║  多策略并行支持:                                          ║
echo  ║    ✅ 同时运行多个策略（不同交易对/不同算法）             ║
echo  ║    ✅ 多 Worker 并行训练（Redis队列）                     ║
echo  ║    ✅ 多交易对同时监控（EventBus分发）                    ║
echo  ║                                                           ║
echo  ║  管理命令:                                                ║
echo  ║    任务管理器 → 结束 gateway-server / python / node       ║
echo  ║    或关闭所有 CMD 窗口                                    ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.
echo  按任意键打开浏览器...
pause >nul
start http://localhost:5173

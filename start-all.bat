@echo off
chcp 65001 >nul
title XiaoTianQuant Trading System
color 0A

:: ============================================================
:: XiaoTianQuant 一键启动脚本
:: 自动配置 VPN 代理，启动所有后台服务
:: ============================================================

echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║         XiaoTianQuant 量化交易系统启动器                  ║
echo  ║         自动配置代理 + 启动所有服务                       ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.

:: ── 配置代理 ──
set HTTP_PROXY=http://127.0.0.1:7897
set HTTPS_PROXY=http://127.0.0.1:7897
set NO_PROXY=localhost,127.0.0.1

echo [1/4] 代理配置: %HTTP_PROXY%

:: ── 检查 VPN 是否运行 ──
echo [2/4] 检查 VPN 代理连通性...
curl -s --proxy %HTTP_PROXY% -o nul -w "%%{http_code}" https://api.binance.com/api/v3/ping > %TEMP%\vpn_test.txt 2>nul
set /p VPN_STATUS=<%TEMP%\vpn_test.txt
del %TEMP%\vpn_test.txt >nul 2>&1

if "%VPN_STATUS%"=="200" (
    echo        VPN 代理正常，Binance API 可访问
) else (
    echo        [WARN] VPN 代理测试失败，请确认 VPN 已开启
    echo        按任意键继续尝试启动...
    pause >nul
)

:: ── 启动 ML Server ──
echo [3/4] 启动 ML Server (Python)...
start "ML Server" cmd /c "cd /d C:\Users\20545\Desktop\xiaotian_quant\sandbox\ml_server && C:\Users\20545\AppData\Local\Programs\Python\Python312\python.exe -m uvicorn main:app --host 0.0.0.0 --port 8001"
timeout /t 3 /nobreak >nul

:: ── 启动 Go Gateway ──
echo [4/4] 启动 Go Gateway...
start "Go Gateway" cmd /c "cd /d C:\Users\20545\Desktop\xiaotian_quant\gateway && set HTTP_PROXY=http://127.0.0.1:7897 && set HTTPS_PROXY=http://127.0.0.1:7897 && gateway-server.exe"
timeout /t 3 /nobreak >nul

:: ── 完成 ──
echo.
echo  ╔══════════════════════════════════════════════════════════╗
echo  ║  所有服务已启动！                                         ║
echo  ║                                                           ║
echo  ║  访问地址: http://localhost:5173                         ║
echo  ║  登录账号: admin / admin123                                ║
echo  ║                                                           ║
echo  ║  数据获取: 自动通过 VPN 代理从 Binance 下载真实数据       ║
echo  ║  无需再运行 sync_data.py 脚本！                           ║
echo  ╚══════════════════════════════════════════════════════════╝
echo.
echo  按任意键打开浏览器...
pause >nul
start http://localhost:5173

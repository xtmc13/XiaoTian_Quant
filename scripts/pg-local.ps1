# 本机便携 PostgreSQL 18 实例启停脚本（仅 Windows 开发环境）。
# 二进制与数据目录位于项目根 .pgsql/（已在 .gitignore 中忽略，不入库）。
#
# 用法:
#   pwsh scripts/pg-local.ps1 start     # 启动（已运行则跳过）
#   pwsh scripts/pg-local.ps1 stop      # 停止（fast 关闭）
#   pwsh scripts/pg-local.ps1 status    # 查看运行状态
#   pwsh scripts/pg-local.ps1 restart   # 重启
#
# 连接口径（与 docker-compose 对齐）：
#   host=127.0.0.1  port=5432  user=xtquant  db=xtquant  trust 免密
#   DATABASE_URL=postgresql://xtquant@127.0.0.1:5432/xtquant?sslmode=disable

param(
    [ValidateSet('start', 'stop', 'status', 'restart')]
    [string]$Action = 'status'
)

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$bin  = Join-Path $root '.pgsql\pgsql\bin'
$data = Join-Path $root '.pgsql\data'
$log  = Join-Path $root '.pgsql\pg.log'
$pgctl = Join-Path $bin 'pg_ctl.exe'

if (-not (Test-Path $pgctl)) {
    Write-Error "未找到便携 PG: $pgctl（请先解压 PostgreSQL 18 binaries 到 .pgsql\pgsql）"
    exit 1
}

function Test-PgRunning {
    $c = New-Object Net.Sockets.TcpClient
    try { $c.Connect('127.0.0.1', 5432); return $true }
    catch { return $false }
    finally { $c.Close() }
}

switch ($Action) {
    'start' {
        if (Test-PgRunning) { Write-Host 'PostgreSQL 已在 127.0.0.1:5432 运行。'; exit 0 }
        Start-Process -FilePath $pgctl -ArgumentList @('-D', $data, '-l', $log, '-w', 'start') -WindowStyle Hidden
        Start-Sleep -Seconds 2
        if (Test-PgRunning) { Write-Host 'PostgreSQL 启动成功: 127.0.0.1:5432 (xtquant/xtquant)' }
        else { Write-Error 'PostgreSQL 启动失败，详见 .pgsql\pg.log' }
    }
    'stop' {
        & $pgctl -D $data stop -m fast
    }
    'status' {
        if (Test-PgRunning) { Write-Host 'PostgreSQL 运行中: 127.0.0.1:5432' }
        else { Write-Host 'PostgreSQL 未运行。' }
    }
    'restart' {
        & $pgctl -D $data restart -m fast
    }
}

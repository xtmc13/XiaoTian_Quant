// 小天量化桌面端主进程
// 形态:Electron 壳加载打包后的 Web 前端。
//   1. 设置 XIAOTIAN_GATEWAY_URL=https://<你的网关地址> 时,加载远程网关托管的 Web(自托管形态)。
//   2. 默认走 file:// 加载本地打包产物(web/build 优先,其次 web/dist —— 本项目 Vite 实际输出为 dist)。
const { app, BrowserWindow, shell } = require('electron')
const path = require('path')
const fs = require('fs')

const REMOTE_URL = process.env.XIAOTIAN_GATEWAY_URL || ''

// vite dev server(electron:dev 使用)
const DEV_URL = process.env.VITE_DEV_SERVER_URL || ''

function resolveLocalIndex() {
  // 开发期:electron/ 位于 web/electron/;打包后:extraResources 拷到 resources/ui/
  const candidates = [
    path.join(__dirname, '..', 'build', 'index.html'),
    path.join(__dirname, '..', 'dist', 'index.html'),
    path.join(process.resourcesPath || '', 'ui', 'build', 'index.html'),
    path.join(process.resourcesPath || '', 'ui', 'dist', 'index.html'),
  ]
  for (const p of candidates) {
    try {
      if (p && fs.existsSync(p)) return p
    } catch (_) { /* ignore */ }
  }
  return null
}

function createWindow() {
  const win = new BrowserWindow({
    width: 1440,
    height: 900,
    minWidth: 1024,
    minHeight: 700,
    title: '小天量化',
    backgroundColor: '#0b1220',
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      contextIsolation: true, // 默认隔离,渲染进程拿不到 Node 能力
      nodeIntegration: false,
      sandbox: true,
    },
  })

  // 外部链接一律交给系统浏览器,不在壳内导航
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https?:\/\//.test(url)) shell.openExternal(url)
    return { action: 'deny' }
  })

  if (DEV_URL) {
    win.loadURL(DEV_URL)
    win.webContents.openDevTools({ mode: 'detach' })
    return
  }

  if (REMOTE_URL) {
    const url = /^https?:\/\//.test(REMOTE_URL) ? REMOTE_URL : `https://${REMOTE_URL}`
    win.loadURL(url.replace(/\/+$/, '/'))
    return
  }

  const index = resolveLocalIndex()
  if (index) {
    win.loadFile(index)
  } else {
    win.loadURL('data:text/html;charset=utf-8,' + encodeURIComponent(
      '<h2 style="font-family:sans-serif">未找到前端构建产物</h2>' +
      '<p>请先执行 <code>npm run build</code>,或设置 <code>XIAOTIAN_GATEWAY_URL</code> 指向远端网关。</p>'
    ))
  }
}

// 单实例锁:重复启动时聚焦已有窗口
const gotLock = app.requestSingleInstanceLock()
if (!gotLock) {
  app.quit()
} else {
  app.on('second-instance', () => {
    const wins = BrowserWindow.getAllWindows()
    if (wins.length) {
      if (wins[0].isMinimized()) wins[0].restore()
      wins[0].focus()
    }
  })

  app.whenReady().then(() => {
    createWindow()
    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow()
    })
  })

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit()
  })
}

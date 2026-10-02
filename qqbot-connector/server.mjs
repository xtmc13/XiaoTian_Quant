// QQ 机器人扫码连接 sidecar。
// 封装 @tencent-connect/qqbot-connector：手机 QQ 扫码后自动获取 AppID/AppSecret，
// 通过 HTTP 提供给 XiaoTian gateway（agentqq 通道）与前端接入面板。
//
// 端点：
//   POST /qr/start    启动（或复用进行中的）扫码会话 → {qr_url}
//   GET  /qr/status   扫码进展 → {state, qr_url?, app_id?, app_secret?, error?}
//   GET  /credentials 已存凭据 → {app_id, app_secret}（未绑定 404）
// 凭据持久化到 /data/qq_credentials.json（gateway_data 卷，重建不丢）。
import express from 'express'
import { startQrConnect } from '@tencent-connect/qqbot-connector'
import fs from 'node:fs'
import path from 'node:path'

const PORT = Number(process.env.PORT || 8787)
const DATA_DIR = process.env.DATA_DIR || '/data'
const CRED_FILE = path.join(DATA_DIR, 'qq_credentials.json')
const SOURCE = process.env.QQ_CONNECTOR_SOURCE || '' // 扫码页展示名，默认"第三方机器人"

/** @type {{state:'idle'|'waiting'|'success'|'error', qr_url:string, app_id:string, app_secret:string, error:string, stop:Function|null}} */
const session = { state: 'idle', qr_url: '', app_id: '', app_secret: '', error: '', stop: null }

// 启动时恢复已存凭据
try {
  const saved = JSON.parse(fs.readFileSync(CRED_FILE, 'utf8'))
  if (saved.app_id && saved.app_secret) {
    session.state = 'success'
    session.app_id = saved.app_id
    session.app_secret = saved.app_secret
  }
} catch { /* 无历史凭据 */ }

function persist() {
  try {
    fs.mkdirSync(DATA_DIR, { recursive: true })
    fs.writeFileSync(CRED_FILE, JSON.stringify({ app_id: session.app_id, app_secret: session.app_secret, saved_at: Date.now() }))
  } catch (e) {
    console.error('[qq-connector] 凭据落盘失败:', e.message)
  }
}

function startQrSession() {
  // 已在扫码/已成功则不重启（成功后可重新扫码换绑：先 stop 再 start）
  if (session.state === 'waiting') return
  if (session.stop) { try { session.stop() } catch {} }
  session.state = 'waiting'
  session.qr_url = ''
  session.error = ''
  session.stop = startQrConnect(
    {
      onSuccess(creds) {
        const c = Array.isArray(creds) ? creds[0] : creds
        if (!c || !c.appId || !c.appSecret) {
          session.state = 'error'
          session.error = '扫码成功但凭据为空'
          return
        }
        session.state = 'success'
        session.app_id = c.appId
        session.app_secret = c.appSecret
        persist()
        console.log('[qq-connector] 绑定成功 appId=', c.appId)
      },
      onFailure(err) {
        // 用户主动 stop 会走这里；仅当是进行中的会话才标 error
        if (session.state === 'waiting') {
          session.state = 'error'
          session.error = err?.message || '扫码绑定失败'
        }
      },
      onQrDisplayed(url) {
        session.qr_url = url
        console.log('[qq-connector] QR 就绪')
      },
      onQrExpired() {
        console.log('[qq-connector] QR 过期，SDK 自动刷新中')
      },
    },
    { displayQrCodeToConsole: false, source: SOURCE },
  )
}

const app = express()
app.use(express.json())

app.post('/qr/start', (_req, res) => {
  if (session.state === 'success' && session.app_id) {
    // 已绑定：默认直接返回凭据态；前端如需换绑先调 /qr/restart
    return res.json({ state: 'success', qr_url: session.qr_url })
  }
  startQrSession()
  // QR URL 由 onQrDisplayed 异步就绪，前端轮询 /qr/status 获取
  res.json({ state: session.state, qr_url: session.qr_url })
})

app.post('/qr/restart', (_req, res) => {
  session.state = 'idle'
  startQrSession()
  res.json({ state: 'waiting', qr_url: session.qr_url })
})

app.get('/qr/status', (_req, res) => {
  res.json({
    state: session.state,
    qr_url: session.qr_url,
    app_id: session.state === 'success' ? session.app_id : undefined,
    app_secret: session.state === 'success' ? session.app_secret : undefined,
    error: session.error || undefined,
  })
})

app.get('/credentials', (_req, res) => {
  if (session.app_id && session.app_secret) {
    res.json({ app_id: session.app_id, app_secret: session.app_secret })
  } else {
    res.status(404).json({ error: 'no credentials' })
  }
})

app.get('/health', (_req, res) => res.json({ ok: true, state: session.state }))

app.listen(PORT, () => console.log(`[qq-connector] listening :${PORT}, state=${session.state}`))

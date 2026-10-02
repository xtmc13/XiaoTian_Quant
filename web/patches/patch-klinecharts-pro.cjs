#!/usr/bin/env node
/**
 * patch-klinecharts-pro.cjs
 *
 * @klinecharts/pro 0.1.1 通过 ref 暴露的图表 API 只有 12 个方法
 *（setTheme/getStyles/setSymbol/setPeriod 等），缺少整页图表交易需要的：
 *   getSize / convertFromPixel —— 图上点击位置 → 价格（点价下单的核心）
 *   scrollToRealTime / setBarSpace —— 视图初始化
 *   updateData / applyNewData —— 实时价推送
 *   subscribeCrosshairChange —— 十字光标联动
 *   getVisibleRange —— 可见区间
 *
 * 内部 klinecharts 实例 `n` 在 ref 字面量作用域内（官方闭包已引用
 * `getStyles: () => n.getStyles()`），这里在同一对象字面量上追加透传方法。
 * npm ci 安装的字节固定，补丁幂等（已打则跳过）。
 */
const fs = require('fs')
const path = require('path')

const distDir = path.join(__dirname, '..', 'node_modules', '@klinecharts', 'pro', 'dist')
if (!fs.existsSync(distDir)) {
  console.error('[patch-pro] dist 不存在:', distDir)
  process.exit(0) // 不阻断构建
}

const PASSTHROUGH = [
  'getSize',
  'convertFromPixel',
  'convertToPixel',
  'scrollToRealTime',
  'setBarSpace',
  'updateData',
  'subscribeCrosshairChange',
  'unsubscribeCrosshairChange',
  'zoomAtDataIndex',
  'setZoomEnabled',
  'getVisibleRange',
  'applyNewData',
  'getDataList',
]

const marker = /getStyles: \(\) => n\.getStyles\(\)/
const patchedFlag = '__xtProPatched'

let patched = 0
for (const name of fs.readdirSync(distDir)) {
  if (!name.endsWith('.js')) continue
  const file = path.join(distDir, name)
  let src = fs.readFileSync(file, 'utf8')
  if (!marker.test(src)) continue
  if (src.includes(patchedFlag)) {
    console.log('[patch-pro] 已打过补丁，跳过:', name)
    continue
  }
  const extra =
    `, ${patchedFlag}: 1` +
    PASSTHROUGH.map((m) => `, ${m}: (...args) => n.${m}.apply(n, args)`).join('')
  src = src.replace(marker, (mm) => mm + extra)
  fs.writeFileSync(file, src)
  patched++
  console.log('[patch-pro] 已修补:', name)
}
console.log(patched === 0 ? '[patch-pro] 未找到补丁点（版本可能已变化，需人工核对）' : `[patch-pro] 完成，共 ${patched} 个文件`)

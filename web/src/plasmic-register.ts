/**
 * 注册现有 React 组件到 Plasmic 可视化编辑器。
 * 注册后可以在 Plasmic Studio 里直接拖拽使用这些组件。
 *
 * 用法: 在 Plasmic Studio 中，点击左侧组件面板 → 导入组件
 */
import { registerComponent } from '@plasmicapp/react-web'
import { PLASMIC } from './plasmic-init'

// ── Trading 组件 ──
// 取消注释即可在 Plasmic 中使用：
//
// import { Trading } from './pages/Trading'
// registerComponent(Trading, {
//   name: 'Trading',
//   displayName: '交易页面',
//   description: '合约/现货交易页面',
//   props: {},
//   importPath: './pages/Trading',
// })

// ── KlineChart ──
// import { KlineChart } from './components/charts/KlineChart'
// registerComponent(KlineChart, {
//   name: 'KlineChart',
//   displayName: 'K线图',
//   props: {
//     symbol: { type: 'string', defaultValue: 'BTCUSDT' },
//     height: { type: 'number', defaultValue: 500 },
//   },
//   importPath: './components/charts/KlineChart',
// })

// ── Dashboard ──
// import { Dashboard } from './pages/Dashboard'
// registerComponent(Dashboard, {
//   name: 'Dashboard',
//   displayName: '仪表盘',
//   props: {},
//   importPath: './pages/Dashboard',
// })

console.log('[Plasmic] Components registered')

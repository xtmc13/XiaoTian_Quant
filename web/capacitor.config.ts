import type { CapacitorConfig } from '@capacitor/cli'

// 小天量化移动端壳配置。
// 形态:App 壳加载打包后的 Web 前端(web/dist),运行时连接用户自托管的网关
// (用户在 App 内填写网关地址),不在 App 内做账户体系。
const config: CapacitorConfig = {
  appId: 'ai.xiaotian.quant',
  appName: '小天量化',
  webDir: 'dist', // Vite 构建输出(npm run build)
  // 局域网真机调试时打开(指向 dev 机器):
  // server: {
  //   url: 'http://192.168.x.x:5173',
  //   cleartext: true,
  // },
  // 生产形态不自填 server —— App 加载本地打包的 Web,由 Web 侧存储的网关地址决定连哪台自托管网关。
  android: {
    allowMixedContent: false,
  },
  ios: {
    contentInset: 'automatic',
  },
}

export default config

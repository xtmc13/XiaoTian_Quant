// 预加载脚本:仅暴露版本信息,不注入任何 Node 能力(contextIsolation 保持默认)。
const { contextBridge } = require('electron')

contextBridge.exposeInMainWorld('xiaotianDesktop', {
  versions: {
    app: process.env.npm_package_version || '',
    electron: process.versions.electron,
    chrome: process.versions.chrome,
    node: process.versions.node,
  },
  platform: process.platform,
})

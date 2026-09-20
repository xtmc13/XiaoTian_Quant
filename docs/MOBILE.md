# 移动端脚手架(Capacitor)

小天量化的 Android / iOS App 基于 [Capacitor](https://capacitorjs.com/) 套壳:
App 内加载打包后的 Web 前端(`web/dist`),运行时连接**用户自托管的网关**。

> 形态说明:本项目不做"账户体系进 App"。App 只是一个壳,用户启动后在界面里填写
> 自己的网关地址(与桌面端 `XIAOTIAN_GATEWAY_URL` 同一形态),所有数据都来自
> 自托管网关。对标 CryptoRobotics 的 Android App。

## 目录与配置

- `web/capacitor.config.ts` — `appId: ai.xiaotian.quant`,`appName: 小天量化`,`webDir: dist`。
- `web/package.json` scripts:
  - `npm run cap:sync` — 把 `web/dist` 同步进原生工程(`npx cap sync` 的别名)
  - `npm run cap:add:android` / `cap:add:ios` — 首次生成 `web/android` / `web/ios` 原生工程
  - `npm run cap:android` / `cap:ios` — 打开 Android Studio / Xcode

## 前置条件

| 平台 | 必需 |
| --- | --- |
| Android | JDK 17、Android Studio(含 Android SDK 34+、Gradle)、一台真机或模拟器(真机需开 USB 调试) |
| iOS | macOS + Xcode 15+、CocoaPods(`sudo gem install cocoapods` 或 `brew install cocoapods`)、Apple 开发者账号(真机调试可用免费个人证书) |

Node 侧仅需 `npm ci`(Capacitor CLI 已在 devDependencies)。

## Android:从零到真机运行

```bash
cd web
npm ci
npm run build            # 产出 web/dist(Vite)

# 首次:生成原生工程(只需一次,之后 npm run cap:sync 增量更新)
npm run cap:add:android  # = npx cap add android

# 每次前端有改动后
npm run cap:sync         # = npx cap sync:拷贝 web/dist + 更新插件/依赖

# 运行
npm run cap:android      # 打开 Android Studio → 选真机/模拟器 → Run ▶
# 或命令行直装已连接真机:
npx cap run android
```

## iOS:从零到真机运行

```bash
cd web
npm ci
npm run build
npm run cap:add:ios      # 首次生成 web/ios(仅 macOS)
npm run cap:sync

npm run cap:ios          # 打开 Xcode
# Xcode 中:选 Team(个人 Apple ID 即可)→ 选真机 → Run ▶
# 首次需在 iPhone 设置 → 通用 → VPN与设备管理 里信任开发者证书
```

## 局域网真机热调试(可选)

把 `web/capacitor.config.ts` 里的 `server` 注释打开,指向开发机的 Vite dev server:

```ts
server: {
  url: 'http://192.168.x.x:5173',   // 开发机局域网 IP
  cleartext: true,                   // Android 允许 http
},
```

然后 `npm run cap:sync`,App 内直接加载 dev server,改代码即时生效。
**发布前务必注释掉 `server` 段**,否则 App 离线不可用。

## 发布构建

- Android:Android Studio → Build → Generate Signed App Bundle / APK,配好 keystore 与 `android/app/build.gradle` 签名配置。
- iOS:Xcode → Product → Archive → Distribute(App Store Connect 或 Ad Hoc)。

## 常见问题

- **`npx cap sync` 报 "webDir is missing"**:先 `npm run build`,确认 `web/dist/index.html` 存在。
- **Android 真机连不上网关**:确认手机与网关服务器同网段/可达,且网关地址带 `http(s)://` 前缀;Android 9+ 默认禁明文 http,仅调试期用 `cleartext: true`。
- **iOS 工程打不开/插件报错**:`cd web/ios/App && pod install` 后重试。

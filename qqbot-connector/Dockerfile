# QQ 扫码连接器 sidecar（封装 @tencent-connect/qqbot-connector）
FROM node:22-alpine
WORKDIR /app
# 只拷贝依赖清单，最大化利用构建缓存
COPY package.json package-lock.json* ./
RUN npm install --omit=dev
COPY server.mjs ./
EXPOSE 8787
CMD ["node", "server.mjs"]

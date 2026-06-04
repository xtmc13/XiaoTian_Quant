import { initPlasmicLoader } from '@plasmicapp/loader-react'

// ⚠️ 需要替换成你的 Plasmic 项目 ID 和 API Token
// Plasmic Studio → Settings → API & Tokens
const PLASMIC_PROJECT_ID = 'YOUR_PROJECT_ID_HERE'
const PLASMIC_API_TOKEN = 'YOUR_API_TOKEN_HERE'

export const PLASMIC = initPlasmicLoader({
  projects: [
    {
      id: PLASMIC_PROJECT_ID,
      token: PLASMIC_API_TOKEN,
    },
  ],
  preview: process.env.NODE_ENV === 'development',
})

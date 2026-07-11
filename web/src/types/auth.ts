export interface User {
  id: string
  username: string
  email?: string
  role: 'admin' | 'user'
}

export interface AuthUser {
  id: string
  username: string
  role: string
  nickname: string
  email?: string
}

export interface AuthLoginResponse {
  access_token: string
  token_type: string
  user: AuthUser
}

export interface UserProfile {
  id: string
  username: string
  nickname: string
  email: string
  role: string
  is_active: number
  email_verified: number
  created_at: string
  credits: number
  is_vip: boolean
  referral_code: string
  referral_count: number
}

export interface NotificationSettings {
  channels: Record<string, boolean>
}

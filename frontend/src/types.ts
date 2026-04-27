export interface User {
  id: string
  username: string
  nickname?: string
  avatar_url?: string
  role: 'admin' | 'user'
}

export interface AuthTokens {
  access_token: string
  refresh_token: string
  token_type: string
  expires_at: string
}

export interface Room {
  id: string
  name: string
  owner_id: string
  visibility: 'public' | 'private'
  current_video_id?: string
  created_at: string
  updated_at: string
  is_owner?: boolean
}

export interface RoomState {
  room_id: string
  video_id?: string
  action: 'play' | 'pause' | 'seek' | 'next' | 'switch'
  position: number
  updated_by?: string
  updated_at?: string
}

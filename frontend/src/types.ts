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

export interface Video {
  id: string
  title: string
  file_path: string
  file_url?: string
  poster_path?: string
  duration: number
  format: string
  size: number
  source_url?: string
  status: 'processing' | 'ready' | 'error'
  created_at: string
  updated_at: string
}

export interface DownloadTask {
  id: string
  user_id: string
  source_url: string
  video_id?: string
  progress: number
  status: 'pending' | 'running' | 'completed' | 'failed' | 'canceled'
  error?: string
  created_at: string
  updated_at: string
}

export interface CapabilityReport {
  ffmpeg: boolean
  ffprobe: boolean
  ytdlp: boolean
  aria2: boolean
  features: {
    hls_download: boolean
    poster_generation: boolean
    ytdlp_import: boolean
    magnet_download: boolean
  }
}

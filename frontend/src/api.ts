import type { AuthTokens, CapabilityReport, DownloadTask, Room, RoomState, User, Video } from './types'

const API_BASE = import.meta.env.VITE_API_BASE ?? ''

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message)
  }
}

export async function apiFetch<T>(path: string, options: RequestInit = {}, token = ''): Promise<T> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json')
  }
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(`${API_BASE}${path}`, { ...options, headers })
  if (!response.ok) {
    let message = response.statusText
    try {
      const payload = await response.json()
      message = payload.error?.message ?? message
    } catch {
      // Keep HTTP status text when the server returned no JSON body.
    }
    throw new ApiError(message, response.status)
  }
  if (response.status === 204) {
    return undefined as T
  }
  return (await response.json()) as T
}

export function register(input: { username: string; password: string; nickname?: string }) {
  return apiFetch<{ user: User; tokens: AuthTokens }>('/api/auth/register', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function login(input: { username: string; password: string }) {
  return apiFetch<{ user: User; tokens: AuthTokens }>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function logout(token: string, refreshToken: string) {
  return apiFetch<void>(
    '/api/auth/logout',
    {
      method: 'POST',
      body: JSON.stringify({ refresh_token: refreshToken }),
    },
    token,
  )
}

export function fetchMe(token: string) {
  return apiFetch<{ user: User }>('/api/users/me', {}, token)
}

export function fetchRooms(token: string) {
  return apiFetch<{ items: Room[]; total: number }>('/api/rooms', {}, token)
}

export function createRoom(
  token: string,
  input: { name: string; visibility: 'public' | 'private'; password?: string },
) {
  return apiFetch<Room & { is_owner: boolean }>(
    '/api/rooms',
    {
      method: 'POST',
      body: JSON.stringify(input),
    },
    token,
  )
}

export function joinRoom(token: string, roomId: string, password = '') {
  return apiFetch<Room & { is_owner: boolean }>(
    `/api/rooms/${roomId}/join`,
    {
      method: 'POST',
      body: JSON.stringify({ password }),
    },
    token,
  )
}

export function fetchRoom(token: string, roomId: string) {
  return apiFetch<Room & { is_owner: boolean }>(`/api/rooms/${roomId}`, {}, token)
}

export function fetchRoomState(token: string, roomId: string) {
  return apiFetch<RoomState>(`/api/rooms/${roomId}/state`, {}, token)
}

export function fetchVideos(token: string, opts: string | { query?: string; status?: string } = '') {
  const query = typeof opts === 'string' ? opts : (opts.query ?? '')
  const status = typeof opts === 'string' ? 'ready' : (opts.status ?? 'ready')
  const params = new URLSearchParams({ limit: '50' })
  if (status) {
    params.set('status', status)
  }
  if (query.trim()) {
    params.set('q', query.trim())
  }
  return apiFetch<{ items: Video[]; total: number }>(`/api/videos?${params}`, {}, token)
}

export function deleteVideo(token: string, id: string) {
  return apiFetch<void>(`/api/admin/videos/${id}`, { method: 'DELETE' }, token)
}

export function fetchDownloads(token: string) {
  return apiFetch<{ items: DownloadTask[] }>('/api/admin/downloads', {}, token)
}

export function createDownload(token: string, sourceUrl: string) {
  return apiFetch<DownloadTask>(
    '/api/admin/downloads',
    {
      method: 'POST',
      body: JSON.stringify({ source_url: sourceUrl }),
    },
    token,
  )
}

export function cancelDownload(token: string, taskId: string) {
  return apiFetch<void>(`/api/admin/downloads/${taskId}`, { method: 'DELETE' }, token)
}

export function kickRoomMember(token: string, roomId: string, userId: string) {
  return apiFetch<void>(`/api/rooms/${roomId}/kick/${userId}`, { method: 'POST' }, token)
}

export function fetchCapabilities(token: string) {
  return apiFetch<CapabilityReport>('/api/capabilities', {}, token)
}

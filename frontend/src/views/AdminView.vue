<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { createDownload, deleteVideo, fetchDownloads, fetchRooms, fetchVideos } from '../api'
import { useAuthStore } from '../stores/auth'
import type { DownloadTask, Room, Video } from '../types'

const auth = useAuthStore()
const videos = ref<Video[]>([])
const downloads = ref<DownloadTask[]>([])
const rooms = ref<Room[]>([])
const sourceUrl = ref('')
const message = ref('')
const wsStatus = ref('未连接')
let socket: WebSocket | null = null

const readyVideos = computed(() => videos.value.filter((video) => video.status === 'ready').length)
const runningTasks = computed(() => downloads.value.filter((task) => ['pending', 'running'].includes(task.status)).length)

async function loadAll() {
  const [videoRes, downloadRes, roomRes] = await Promise.all([
    fetchVideos(auth.accessToken.value),
    fetchDownloads(auth.accessToken.value),
    fetchRooms(auth.accessToken.value),
  ])
  videos.value = videoRes.items
  downloads.value = downloadRes.items
  rooms.value = roomRes.items
}

async function submitDownload() {
  if (!sourceUrl.value.trim()) return
  const task = await createDownload(auth.accessToken.value, sourceUrl.value)
  downloads.value = [task, ...downloads.value.filter((item) => item.id !== task.id)]
  sourceUrl.value = ''
  message.value = '下载任务已提交'
}

async function removeVideo(video: Video) {
  await deleteVideo(auth.accessToken.value, video.id)
  videos.value = videos.value.filter((item) => item.id !== video.id)
}

function connectDownloadSocket() {
  if (socket) socket.close()
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${protocol}//${window.location.host}/ws/admin/downloads?token=${encodeURIComponent(auth.accessToken.value)}`
  socket = new WebSocket(url)
  wsStatus.value = '连接中'
  socket.onopen = () => {
    wsStatus.value = '已连接'
  }
  socket.onclose = () => {
    wsStatus.value = '已断开'
  }
  socket.onmessage = (event) => {
    const payload = JSON.parse(event.data)
    if (payload.type !== 'download_task') return
    const task = payload.task as DownloadTask
    downloads.value = [task, ...downloads.value.filter((item) => item.id !== task.id)]
  }
}

onMounted(async () => {
  await loadAll()
  connectDownloadSocket()
})

onUnmounted(() => {
  socket?.close()
})
</script>

<template>
  <section class="admin-stack">
    <div class="stats-grid">
      <article class="panel stat-card">
        <p class="eyebrow">视频库</p>
        <strong>{{ videos.length }}</strong>
        <span>{{ readyVideos }} 个可播放</span>
      </article>
      <article class="panel stat-card">
        <p class="eyebrow">下载任务</p>
        <strong>{{ downloads.length }}</strong>
        <span>{{ runningTasks }} 个进行中</span>
      </article>
      <article class="panel stat-card">
        <p class="eyebrow">房间监控</p>
        <strong>{{ rooms.length }}</strong>
        <span>当前房间</span>
      </article>
    </div>

    <section class="panel">
      <div class="section-head">
        <div>
          <p class="eyebrow">下载管理</p>
          <h2>提交 URL 并监听实时进度</h2>
        </div>
        <span class="pill">WebSocket {{ wsStatus }}</span>
      </div>
      <form class="inline-form" @submit.prevent="submitDownload">
        <input v-model="sourceUrl" placeholder="https://example.com/movie.mp4 或 m3u8 / 站点链接" />
        <button type="submit">提交下载</button>
      </form>
      <p v-if="message" class="success">{{ message }}</p>
      <div class="table-list">
        <div class="table-row" v-for="task in downloads" :key="task.id">
          <div>
            <strong>{{ task.source_url }}</strong>
            <small>{{ task.status }} · {{ Math.round(task.progress) }}%</small>
          </div>
          <progress max="100" :value="task.progress"></progress>
        </div>
        <p v-if="downloads.length === 0" class="muted">暂无下载任务。</p>
      </div>
    </section>

    <section class="panel">
      <div class="section-head">
        <div>
          <p class="eyebrow">视频库管理</p>
          <h2>服务器缓存视频</h2>
        </div>
        <button class="secondary" @click="loadAll">刷新</button>
      </div>
      <div class="video-library">
        <article class="video-card" v-for="video in videos" :key="video.id">
          <img v-if="video.poster_path" :src="video.poster_path" :alt="video.title" />
          <div v-else class="poster-placeholder">无封面</div>
          <strong>{{ video.title }}</strong>
          <small>{{ video.format || 'unknown' }} · {{ Math.round(video.duration || 0) }} 秒 · {{ video.status }}</small>
          <button class="danger" @click="removeVideo(video)">删除</button>
        </article>
        <p v-if="videos.length === 0" class="muted">暂无缓存视频。</p>
      </div>
    </section>

    <section class="panel">
      <p class="eyebrow">房间监控</p>
      <h2>当前房间</h2>
      <div class="table-list">
        <div class="table-row" v-for="room in rooms" :key="room.id">
          <div>
            <strong>{{ room.name }}</strong>
            <small>{{ room.visibility }} · owner {{ room.owner_id }}</small>
          </div>
          <span class="pill">{{ room.current_video_id || '未选择视频' }}</span>
        </div>
      </div>
    </section>
  </section>
</template>

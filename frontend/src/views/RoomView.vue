<script setup lang="ts">
import Hls from 'hls.js'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchRoomState, fetchVideos, kickRoomMember } from '../api'
import { useRoomSocket, type RoomSocketMessage } from '../composables/useRoomSocket'
import { useAuthStore } from '../stores/auth'
import type { Room, RoomState, Video } from '../types'

const props = defineProps<{ room: Room & { is_owner?: boolean } }>()
const emit = defineEmits<{ back: [] }>()
const auth = useAuthStore()
const state = ref<RoomState | null>(null)
const position = ref(0)
const currentVideo = ref(props.room.current_video_id || '')
const manualUrl = ref('')
const library = ref<Video[]>([])
const queue = ref<Video[]>([])
const videoElement = ref<HTMLVideoElement | null>(null)
let hls: Hls | null = null
const currentUser = computed(() => auth.user.value)
const members = ref<string[]>([currentUser.value?.username ?? 'me'])
const canControl = computed(() => currentUser.value?.role === 'admin' || props.room.is_owner || props.room.owner_id === currentUser.value?.id)
const socket = useRoomSocket(() => props.room.id, () => auth.accessToken.value)
const selectedVideo = computed(() => queue.value.find((item) => item.id === currentVideo.value) ?? library.value.find((item) => item.id === currentVideo.value))
const playbackUrl = computed(() => {
  if (!selectedVideo.value) return currentVideo.value
  return selectedVideo.value.file_url || `/api/videos/${selectedVideo.value.id}/file`
})

onMounted(async () => {
  const [roomState, videos] = await Promise.all([
    fetchRoomState(auth.accessToken.value, props.room.id),
    fetchVideos(auth.accessToken.value, { status: 'ready' }),
  ])
  state.value = roomState
  library.value = videos.items
  queue.value = videos.items.slice(0, 5)
  position.value = state.value.position
  currentVideo.value = state.value.video_id || currentVideo.value || queue.value[0]?.id || ''
  socket.connect()
  await nextTick()
  loadPlaybackSource()
})

onBeforeUnmount(() => hls?.destroy())

watch(socket.lastMessage, (message) => {
  if (!message) return
  if (message.type === 'sync') {
    state.value = {
      room_id: props.room.id,
      action: message.action ?? 'pause',
      position: message.position ?? 0,
      video_id: message.video_id ?? '',
      updated_at: new Date((message.timestamp ?? Date.now() / 1000) * 1000).toISOString(),
    }
    position.value = state.value.position
    currentVideo.value = state.value.video_id ?? ''
    syncPlayer(message.action ?? 'pause', state.value.position)
  }
  if (message.type === 'room_event' && message.user?.username && !members.value.includes(message.user.username)) {
    members.value.push(message.user.username)
  }
})

function send(action: RoomSocketMessage['action']) {
  if (!canControl.value) return
  position.value = videoElement.value?.currentTime ?? Number(position.value)
  socket.sendControl(action, Number(position.value), currentVideo.value)
  syncPlayer(action, Number(position.value))
}

watch(playbackUrl, () => loadPlaybackSource())

function loadPlaybackSource() {
  const video = videoElement.value
  if (!video || !playbackUrl.value) return
  hls?.destroy()
  hls = null
  const url = playbackUrl.value
  if (url.endsWith('.m3u8') && Hls.isSupported()) {
    hls = new Hls()
    hls.loadSource(url)
    hls.attachMedia(video)
    return
  }
  video.src = url
  video.load()
}

function syncPlayer(action: RoomSocketMessage['action'], nextPosition: number) {
  const video = videoElement.value
  if (!video) return
  if (Number.isFinite(nextPosition)) {
    video.currentTime = nextPosition
  }
  if (action === 'play') {
    video.play().catch(() => undefined)
  }
  if (action === 'pause') {
    video.pause()
  }
}

function addManualUrl() {
  if (!canControl.value || !manualUrl.value.trim()) return
  const url = manualUrl.value.trim()
  queue.value.push({
    id: url,
    title: url.split('/').pop() || url,
    file_path: url,
    file_url: url,
    duration: 0,
    format: url.split('.').pop() || '',
    size: 0,
    status: 'ready',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  })
  manualUrl.value = ''
}

function addLibraryVideo(video: Video) {
  if (!canControl.value || queue.value.some((item) => item.id === video.id)) return
  queue.value.push(video)
}

function moveQueue(index: number, delta: number) {
  if (!canControl.value) return
  const target = index + delta
  if (target < 0 || target >= queue.value.length) return
  const items = [...queue.value]
  const [item] = items.splice(index, 1)
  items.splice(target, 0, item)
  queue.value = items
}

function removeQueue(index: number) {
  if (!canControl.value) return
  queue.value.splice(index, 1)
}

async function kick(member: string) {
  if (!canControl.value) return
  await kickRoomMember(auth.accessToken.value, props.room.id, member)
  members.value = members.value.filter((item) => item !== member)
}
</script>

<template>
  <section class="room-grid">
    <div class="panel player-panel">
      <button class="link" @click="emit('back')">← 返回大厅</button>
      <div class="video-frame">
        <video ref="videoElement" controls playsinline @timeupdate="position = videoElement?.currentTime ?? 0"></video>
      </div>
      <p class="muted">当前视频：{{ selectedVideo?.title || currentVideo || '尚未选择视频' }}</p>
      <div class="controls" :class="{ disabled: !canControl }">
        <button @click="send('play')" :disabled="!canControl">播放</button>
        <button @click="send('pause')" :disabled="!canControl">暂停</button>
        <button @click="send('seek')" :disabled="!canControl">同步进度</button>
        <label>
          进度
          <input v-model.number="position" type="range" min="0" max="7200" :disabled="!canControl" />
        </label>
      </div>
      <p class="hint" v-if="!canControl">普通成员为只读模式，等待房主或管理员同步播放。</p>
      <p class="hint" v-else>你拥有播放控制权限，操作会通过 WebSocket 广播。</p>
    </div>

    <aside class="panel side-panel">
      <h3>视频队列</h3>
      <form class="inline-form" @submit.prevent="addManualUrl">
        <input v-model="manualUrl" placeholder="添加 mp4/m3u8 URL" :disabled="!canControl" />
        <button :disabled="!canControl">添加</button>
      </form>
      <div class="queue-item" v-for="(item, index) in queue" :key="item.id">
        <div>
          <strong>{{ item.title }}</strong>
          <small>{{ item.file_url || item.file_path }}</small>
        </div>
        <div class="queue-actions">
          <button :disabled="!canControl || index === 0" @click="moveQueue(index, -1)">↑</button>
          <button :disabled="!canControl || index === queue.length - 1" @click="moveQueue(index, 1)">↓</button>
          <button :disabled="!canControl" @click="currentVideo = item.id; send('switch')">切换</button>
          <button :disabled="!canControl" @click="removeQueue(index)">删除</button>
        </div>
      </div>
      <h3>视频库</h3>
      <div class="queue-item" v-for="video in library" :key="video.id">
        <div>
          <strong>{{ video.title }}</strong>
          <small>{{ Math.round(video.duration || 0) }} 秒 · {{ video.format || 'unknown' }}</small>
        </div>
        <button :disabled="!canControl" @click="addLibraryVideo(video)">加入队列</button>
      </div>
      <h3>在线成员</h3>
      <div class="member" v-for="member in members" :key="member">
        <span class="avatar">{{ member.slice(0, 1).toUpperCase() }}</span>
        {{ member }}
        <button v-if="canControl && member !== currentUser?.username" @click="kick(member)">踢出</button>
      </div>
      <h3>实时事件</h3>
      <pre>{{ socket.events.value.slice(0, 5) }}</pre>
    </aside>
  </section>
</template>

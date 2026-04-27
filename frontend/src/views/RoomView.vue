<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { fetchRoomState } from '../api'
import { useRoomSocket, type RoomSocketMessage } from '../composables/useRoomSocket'
import { useAuthStore } from '../stores/auth'
import type { Room, RoomState } from '../types'

const props = defineProps<{ room: Room & { is_owner?: boolean } }>()
const emit = defineEmits<{ back: [] }>()
const auth = useAuthStore()
const state = ref<RoomState | null>(null)
const position = ref(0)
const currentVideo = ref(props.room.current_video_id || '')
const queue = ref([
  { id: 'sample-hls', title: '示例 HLS 片源', url: '/static/videos/sample.m3u8' },
  { id: 'sample-mp4', title: '示例 MP4 片源', url: '/static/videos/sample.mp4' },
])
const currentUser = computed(() => auth.user.value)
const members = ref<string[]>([currentUser.value?.username ?? 'me'])
const canControl = computed(() => currentUser.value?.role === 'admin' || props.room.is_owner || props.room.owner_id === currentUser.value?.id)
const socket = useRoomSocket(() => props.room.id, () => auth.accessToken.value)

onMounted(async () => {
  state.value = await fetchRoomState(auth.accessToken.value, props.room.id)
  position.value = state.value.position
  currentVideo.value = state.value.video_id || currentVideo.value
  socket.connect()
})

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
  }
  if (message.type === 'room_event' && message.user?.username && !members.value.includes(message.user.username)) {
    members.value.push(message.user.username)
  }
})

function send(action: RoomSocketMessage['action']) {
  if (!canControl.value) return
  socket.sendControl(action, Number(position.value), currentVideo.value)
}
</script>

<template>
  <section class="room-grid">
    <div class="panel player-panel">
      <button class="link" @click="emit('back')">← 返回大厅</button>
      <div class="video-frame">
        <div>
          <p class="eyebrow">播放器</p>
          <h2>{{ room.name }}</h2>
          <p>{{ currentVideo || '尚未选择视频' }}</p>
        </div>
      </div>
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
      <div class="queue-item" v-for="item in queue" :key="item.id">
        <div>
          <strong>{{ item.title }}</strong>
          <small>{{ item.url }}</small>
        </div>
        <button :disabled="!canControl" @click="currentVideo = item.id; send('switch')">切换</button>
      </div>
      <h3>在线成员</h3>
      <div class="member" v-for="member in members" :key="member">
        <span class="avatar">{{ member.slice(0, 1).toUpperCase() }}</span>
        {{ member }}
        <button v-if="canControl && member !== currentUser?.username">踢出</button>
      </div>
      <h3>实时事件</h3>
      <pre>{{ socket.events.value.slice(0, 5) }}</pre>
    </aside>
  </section>
</template>

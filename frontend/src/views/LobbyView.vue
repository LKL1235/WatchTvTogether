<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { createRoom as createRoomRequest, fetchRooms, joinRoom } from '../api'
import { useAuthStore } from '../stores/auth'
import type { Room } from '../types'

const emit = defineEmits<{ 'open-room': [room: Room] }>()
const auth = useAuthStore()
const rooms = ref<Room[]>([])
const loading = ref(false)
const error = ref('')
const form = ref({ name: '', visibility: 'public' as 'public' | 'private', password: '' })

async function loadRooms() {
  loading.value = true
  error.value = ''
  try {
    const res = await fetchRooms(auth.accessToken.value)
    rooms.value = res.items
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载房间失败'
  } finally {
    loading.value = false
  }
}

async function createRoom() {
  if (!form.value.name.trim()) return
  const room = await createRoomRequest(auth.accessToken.value, {
    name: form.value.name,
    visibility: form.value.visibility,
    password: form.value.password,
  })
  form.value = { name: '', visibility: 'public', password: '' }
  await loadRooms()
  emit('open-room', room)
}

async function openRoom(room: Room) {
  try {
    const joined = await joinRoom(auth.accessToken.value, room.id)
    emit('open-room', joined)
  } catch (err) {
    const password = window.prompt('请输入私有房间密码') ?? ''
    if (!password) return
    const joined = await joinRoom(auth.accessToken.value, room.id, password)
    emit('open-room', joined)
  }
}

onMounted(loadRooms)
</script>

<template>
  <section class="grid two">
    <article class="panel">
      <p class="eyebrow">大厅</p>
      <h2>公开房间</h2>
      <p v-if="error" class="error">{{ error }}</p>
      <p v-if="loading" class="muted">加载中...</p>
      <div v-else class="room-list">
        <button v-for="room in rooms" :key="room.id" class="room-card" @click="openRoom(room)">
          <strong>{{ room.name }}</strong>
          <span>{{ room.visibility === 'private' ? '私有' : '公开' }}</span>
        </button>
        <p v-if="rooms.length === 0" class="muted">暂无房间，创建一个开始同步观影。</p>
      </div>
    </article>

    <article class="panel">
      <p class="eyebrow">创建房间</p>
      <h2>房间创建弹窗</h2>
      <form class="form" @submit.prevent="createRoom">
        <label>房间名<input v-model="form.name" required placeholder="周五电影夜" /></label>
        <label>可见性
          <select v-model="form.visibility">
            <option value="public">公开</option>
            <option value="private">私有</option>
          </select>
        </label>
        <label v-if="form.visibility === 'private'">密码<input v-model="form.password" type="password" /></label>
        <button type="submit">创建并进入</button>
      </form>
    </article>
  </section>
</template>

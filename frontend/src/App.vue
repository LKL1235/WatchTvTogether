<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAuthStore } from './stores/auth'
import AuthView from './views/AuthView.vue'
import LobbyView from './views/LobbyView.vue'
import RoomView from './views/RoomView.vue'
import AdminView from './views/AdminView.vue'
import type { Room } from './types'

const auth = useAuthStore()
const currentRoom = ref<Room | null>(null)
const showAdmin = ref(false)
const isAdmin = computed(() => auth.user.value?.role === 'admin')

onMounted(() => {
  auth.loadMe().catch(() => auth.logout())
})
</script>

<template>
  <AuthView v-if="!auth.isAuthenticated.value" />
  <main v-else class="app-shell">
    <header class="topbar">
      <div>
        <p class="eyebrow">WatchTogether</p>
        <h1>同步观影控制台</h1>
      </div>
      <nav>
        <button :class="{ active: !currentRoom && !showAdmin }" @click="currentRoom = null; showAdmin = false">大厅</button>
        <button v-if="isAdmin" :class="{ active: showAdmin }" @click="showAdmin = true">管理员后台</button>
        <button @click="auth.logout()">退出 {{ auth.user.value?.username }}</button>
      </nav>
    </header>

    <AdminView v-if="showAdmin" @open-room="currentRoom = $event; showAdmin = false" />
    <RoomView v-else-if="currentRoom" :room="currentRoom" @back="currentRoom = null" />
    <LobbyView v-else @open-room="currentRoom = $event" />
  </main>
</template>

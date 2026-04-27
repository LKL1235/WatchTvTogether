<script setup lang="ts">
import { ref } from 'vue'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const mode = ref<'login' | 'register'>('login')
const username = ref('')
const password = ref('')
const nickname = ref('')
const error = ref('')
const loading = ref(false)

async function submit() {
  error.value = ''
  loading.value = true
  try {
    if (mode.value === 'login') {
      await auth.login({ username: username.value, password: password.value })
    } else {
      await auth.register({ username: username.value, password: password.value, nickname: nickname.value })
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '请求失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-card">
      <p class="eyebrow">WatchTogether</p>
      <h1>{{ mode === 'login' ? '登录' : '注册' }}</h1>
      <form @submit.prevent="submit">
        <label>
          用户名
          <input v-model="username" autocomplete="username" required minlength="3" />
        </label>
        <label v-if="mode === 'register'">
          昵称
          <input v-model="nickname" autocomplete="nickname" />
        </label>
        <label>
          密码
          <input v-model="password" type="password" autocomplete="current-password" required minlength="8" />
        </label>
        <p v-if="error" class="error">{{ error }}</p>
        <button class="primary" :disabled="loading">{{ loading ? '处理中...' : '继续' }}</button>
      </form>
      <button class="link" @click="mode = mode === 'login' ? 'register' : 'login'">
        {{ mode === 'login' ? '没有账号？去注册' : '已有账号？去登录' }}
      </button>
    </section>
  </main>
</template>

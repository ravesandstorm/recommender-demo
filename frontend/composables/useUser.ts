import type { User } from './useApi'

const userId = ref<string>('')
const users = ref<User[]>([])

export function useUser() {
  function loadFromStorage() {
    if (!import.meta.client) return
    const saved = localStorage.getItem('recsys_user_id')
    if (saved) userId.value = saved
  }

  function selectUser(id: string) {
    userId.value = id
    if (import.meta.client) {
      localStorage.setItem('recsys_user_id', id)
    }
  }

  async function refreshUsers() {
    const api = useApi()
    users.value = await api.listUsers()
    if (userId.value && !users.value.some(u => u.id === userId.value)) {
      userId.value = ''
      if (import.meta.client) localStorage.removeItem('recsys_user_id')
    }
  }

  async function createUser(username: string) {
    const api = useApi()
    const u = await api.createUser(username.trim())
    users.value = [...users.value, u]
    selectUser(u.id)
    return u
  }

  return { userId, users, loadFromStorage, selectUser, refreshUsers, createUser }
}

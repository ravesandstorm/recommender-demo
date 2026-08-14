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

  function clearUser() {
    userId.value = ''
    if (import.meta.client) localStorage.removeItem('recsys_user_id')
  }

  async function refreshUsers() {
    const api = useApi()
    users.value = await api.listUsers()
    if (userId.value && users.value.length > 0 && !users.value.some(u => u.id === userId.value)) {
      clearUser()
    }
  }

  async function createUser(username: string) {
    const api = useApi()
    const u = await api.createUser(username.trim())
    users.value = [...users.value, u]
    selectUser(u.id)
    return u
  }

  async function deleteUser(id: string) {
    const api = useApi()
    await api.deleteUser(id)
    users.value = users.value.filter(u => u.id !== id)
    if (userId.value === id) {
      clearUser()
    }
  }

  return { userId, users, loadFromStorage, selectUser, clearUser, refreshUsers, createUser, deleteUser }
}

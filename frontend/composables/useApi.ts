export type User = {
  id: string
  username: string
  created_at: string
}

export type Post = {
  id: string
  title: string
  content: string
}

export type Comment = {
  id: string
  user_id: string
  post_id: string
  body: string
  created_at: string
  username?: string
}

export function useApi() {
  const config = useRuntimeConfig()
  const base = config.public.apiBase as string
  const { userId } = useUser()

  async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers = new Headers(options.headers || {})
    headers.set('Accept', 'application/json')
    if (options.body && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json')
    }
    if (userId.value) {
      headers.set('X-User-ID', userId.value)
    }
    const res = await fetch(`${base}${path}`, { ...options, headers })
    if (!res.ok) {
      const text = await res.text()
      throw new Error(text || res.statusText)
    }
    if (res.status === 204) {
      return undefined as T
    }
    return res.json() as Promise<T>
  }

  return {
    listUsers: () => api<User[]>('/api/users'),
    createUser: (username: string) =>
      api<User>('/api/users', { method: 'POST', body: JSON.stringify({ username }) }),
    fetchFeed: () => api<{ posts: Post[] }>('/api/feed'),
    like: (postId: string) => api(`/api/posts/${postId}/like`, { method: 'POST' }),
    unlike: (postId: string) => api(`/api/posts/${postId}/like`, { method: 'DELETE' }),
    dislike: (postId: string) => api(`/api/posts/${postId}/dislike`, { method: 'POST' }),
    undislike: (postId: string) => api(`/api/posts/${postId}/dislike`, { method: 'DELETE' }),
    save: (postId: string) => api(`/api/posts/${postId}/save`, { method: 'POST' }),
    unsave: (postId: string) => api(`/api/posts/${postId}/save`, { method: 'DELETE' }),
    share: (postId: string) => api(`/api/posts/${postId}/share`, { method: 'POST' }),
    listComments: (postId: string) => api<Comment[]>(`/api/posts/${postId}/comments`),
    createComment: (postId: string, body: string) =>
      api<Comment>(`/api/posts/${postId}/comments`, {
        method: 'POST',
        body: JSON.stringify({ body }),
      }),
    getConfig: () => api<Record<string, unknown>>('/api/config'),
  }
}

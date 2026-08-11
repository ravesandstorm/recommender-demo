<template>
  <div class="app-shell">
    <header class="topbar">
      <div class="brand">Recsys Demo</div>
      <div class="user-controls">
        <select
          :value="userId"
          @change="selectUser(($event.target as HTMLSelectElement).value)"
        >
          <option value="" disabled>Select user</option>
          <option v-for="u in users" :key="u.id" :value="u.id">{{ u.username }}</option>
        </select>
        <input v-model="newUsername" placeholder="New username" @keyup.enter="onCreateUser" />
        <button class="btn btn-primary" :disabled="creatingUser" @click="onCreateUser">
          Create user
        </button>
      </div>
    </header>

    <p v-if="error" class="status" style="color: var(--danger)">{{ error }}</p>

    <div v-if="!userId" class="empty-hint">
      Create or select a user to start the infinite recommendation feed.
    </div>

    <div v-else class="feed">
      <article v-for="post in posts" :key="post.id" class="post">
        <h2>{{ post.title }}</h2>
        <div class="content">{{ post.content }}</div>
        <div class="actions">
          <button class="btn" :class="{ active: liked[post.id] }" @click="toggleLike(post.id)">Like</button>
          <button class="btn danger" :class="{ active: disliked[post.id] }" @click="toggleDislike(post.id)">Dislike</button>
          <button class="btn" :class="{ active: saved[post.id] }" @click="toggleSave(post.id)">Save</button>
          <button class="btn" @click="sharePost(post.id)">Share</button>
          <button class="btn" @click="openComments(post)">Comment</button>
        </div>
      </article>

      <div ref="sentinel" class="sentinel" />
      <p v-if="loading" class="status">Loading recommendations…</p>
      <p v-else-if="exhausted" class="status">No more posts for this user.</p>
    </div>

    <div v-if="commentOpen && commentPost" class="modal-backdrop" @click.self="closeComments">
      <div class="modal" role="dialog" aria-modal="true">
        <header>
          <h3>Comments — {{ commentPost.title }}</h3>
          <button class="btn" @click="closeComments">Close</button>
        </header>
        <p v-if="commentLoading" class="status">Loading…</p>
        <div v-else class="comments">
          <div v-if="!comments.length" class="status">No comments yet.</div>
          <div v-for="c in comments" :key="c.id" class="comment">
            <div class="meta">{{ c.username || c.user_id }} · {{ new Date(c.created_at).toLocaleString() }}</div>
            <div>{{ c.body }}</div>
          </div>
        </div>
        <form class="comment-form" @submit.prevent="submitComment">
          <input v-model="commentBody" placeholder="Write a comment" :disabled="!userId" />
          <button class="btn btn-primary" type="submit">Post</button>
        </form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Comment, Post } from '~/composables/useApi'

const api = useApi()
const { userId, users, loadFromStorage, selectUser, refreshUsers, createUser } = useUser()
const { run: debounce } = useDebouncedAction(300)

const posts = ref<Post[]>([])
const loading = ref(false)
const exhausted = ref(false)
const error = ref('')
const newUsername = ref('')
const creatingUser = ref(false)

const liked = reactive<Record<string, boolean>>({})
const disliked = reactive<Record<string, boolean>>({})
const saved = reactive<Record<string, boolean>>({})

const commentOpen = ref(false)
const commentPost = ref<Post | null>(null)
const comments = ref<Comment[]>([])
const commentBody = ref('')
const commentLoading = ref(false)

const sentinel = ref<HTMLElement | null>(null)

onMounted(async () => {
  loadFromStorage()
  if (userId.value && posts.value.length === 0 && !loading.value) {
    await loadMore()
  }
  try {
    await refreshUsers()
  } catch (e) {
    error.value = 'Cannot reach API. Is the backend running on :8090?'
  }
})

watch(userId, async (id, prev) => {
  if (!id || id === prev) return
  posts.value = []
  exhausted.value = false
  Object.keys(liked).forEach(k => delete liked[k])
  Object.keys(disliked).forEach(k => delete disliked[k])
  Object.keys(saved).forEach(k => delete saved[k])
  await loadMore()
})

async function onCreateUser() {
  if (!newUsername.value.trim()) return
  creatingUser.value = true
  error.value = ''
  try {
    await createUser(newUsername.value)
    newUsername.value = ''
  } catch (e: any) {
    error.value = e?.message || 'Failed to create user'
  } finally {
    creatingUser.value = false
  }
}

async function loadMore() {
  if (!userId.value || loading.value || exhausted.value) return
  loading.value = true
  error.value = ''
  try {
    const res = await api.fetchFeed()
    if (!res.posts?.length) {
      exhausted.value = true
    } else {
      posts.value.push(...res.posts)
      if (res.posts.length < 5) exhausted.value = true
    }
  } catch (e: any) {
    error.value = e?.message || 'Failed to load feed'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  const obs = new IntersectionObserver((entries) => {
    if (entries.some(e => e.isIntersecting)) loadMore()
  }, { rootMargin: '240px' })
  watch(sentinel, (el, _, onCleanup) => {
    if (!el) return
    obs.observe(el)
    onCleanup(() => obs.unobserve(el))
  }, { immediate: true })
})

function toggleLike(postId: string) {
  const next = !liked[postId]
  liked[postId] = next
  if (next) disliked[postId] = false
  debounce(`like:${postId}`, async () => {
    if (liked[postId]) await api.like(postId)
    else await api.unlike(postId)
  })
}

function toggleDislike(postId: string) {
  const next = !disliked[postId]
  disliked[postId] = next
  if (next) liked[postId] = false
  debounce(`dislike:${postId}`, async () => {
    if (disliked[postId]) await api.dislike(postId)
    else await api.undislike(postId)
  })
}

function toggleSave(postId: string) {
  const next = !saved[postId]
  saved[postId] = next
  debounce(`save:${postId}`, async () => {
    if (saved[postId]) await api.save(postId)
    else await api.unsave(postId)
  })
}

function sharePost(postId: string) {
  debounce(`share:${postId}`, async () => {
    await api.share(postId)
  })
}

async function openComments(post: Post) {
  commentPost.value = post
  commentOpen.value = true
  commentBody.value = ''
  commentLoading.value = true
  try {
    comments.value = await api.listComments(post.id)
  } catch (e: any) {
    error.value = e?.message || 'Failed to load comments'
  } finally {
    commentLoading.value = false
  }
}

function closeComments() {
  commentOpen.value = false
  commentPost.value = null
}

async function submitComment() {
  if (!commentPost.value || !commentBody.value.trim() || !userId.value) return
  const body = commentBody.value.trim()
  commentBody.value = ''
  try {
    const c = await api.createComment(commentPost.value.id, body)
    comments.value = [...comments.value, c]
  } catch (e: any) {
    error.value = e?.message || 'Failed to comment'
  }
}
</script>

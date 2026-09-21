<template>
  <section class="comments">
    <h3 class="comments-title">评论（{{ total }}）</h3>

    <p v-if="loading" class="comments-tip">加载中…</p>
    <p v-else-if="loadError" class="comments-tip error">
      {{ loadError }}
      <button class="retry-btn" @click="load">重试</button>
    </p>
    <p v-else-if="!tree.length" class="comments-tip">暂无评论，来发表第一条评论吧</p>

    <ul v-else class="comment-list">
      <CommentItem
        v-for="c in tree"
        :key="c.id"
        :comment="c"
        :expanded="expanded.has(c.id)"
        :reply-box-open="replyBoxFor === c.id"
        :reply-content="replyContent"
        :posting="posting"
        :my-user-id="myUserId"
        @toggle-replies="toggleReplies"
        @toggle-reply-box="toggleReplyBox"
        @update:reply-content="replyContent = $event"
        @submit-reply="submitReply"
        @remove="remove"
      />
    </ul>

    <div class="post-box">
      <input
        v-model.trim="newContent"
        class="input"
        type="text"
        maxlength="500"
        placeholder="写下你的评论…"
        @keyup.enter="submitNew"
      />
      <button class="btn primary" :disabled="!newContent || posting" @click="submitNew">
        {{ posting ? '发送中…' : '发表评论' }}
      </button>
    </div>
    <p v-if="postError" class="post-error">{{ postError }}</p>
  </section>
</template>

<script setup>
// 相册评论面板——列表项抽为 CommentItem（Job000058-5 拆分）。
// 两级树/全局唯一回复框/删除权限判定仍在此，行为语义不变。
import { computed, onMounted, reactive, ref } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { createComment, deleteComment, errMsg, listComments } from './albumApi'
import CommentItem from './CommentItem.vue'

const props = defineProps({
  albumId: { type: [String, Number], required: true }
})

const auth = useAuthStore()

const comments = ref([])
const loading = ref(false)
const loadError = ref('')
const newContent = ref('')
const replyContent = ref('')
const replyBoxFor = ref('')
const posting = ref(false)
const postError = ref('')
const expanded = reactive(new Set())

const myUserId = computed(() => auth.user?.id || auth.user?.user_id || '')

// 扁平升序 → 两级树（回复统一挂到顶层评论下）
const tree = computed(() => {
  const tops = []
  const byId = new Map()
  for (const c of comments.value) {
    if (!c.parent_id) {
      const node = { ...c, replies: [] }
      tops.push(node)
      byId.set(c.id, node)
    }
  }
  for (const c of comments.value) {
    if (!c.parent_id) continue
    const parent = byId.get(c.parent_id)
    if (parent) parent.replies.push(c)
    else tops.push({ ...c, replies: [] }) // 父评论缺失时兜底为顶层
  }
  return tops
})

const total = computed(() => comments.value.length)

function toggleReplies(id) {
  if (expanded.has(id)) expanded.delete(id)
  else expanded.add(id)
}

function toggleReplyBox(id) {
  replyBoxFor.value = replyBoxFor.value === id ? '' : id
  replyContent.value = ''
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    comments.value = await listComments(props.albumId)
  } catch (e) {
    loadError.value = errMsg(e, '评论加载失败')
  } finally {
    loading.value = false
  }
}

async function submitNew() {
  if (!newContent.value || posting.value) return
  await post(newContent.value, null)
  newContent.value = ''
}

async function submitReply(parentId) {
  if (!replyContent.value || posting.value) return
  await post(replyContent.value, parentId)
  replyBoxFor.value = ''
  replyContent.value = ''
  expanded.add(parentId)
}

async function post(content, parentId) {
  posting.value = true
  postError.value = ''
  try {
    await createComment(props.albumId, content, parentId)
    await load()
  } catch (e) {
    postError.value = errMsg(e, '评论发送失败')
  } finally {
    posting.value = false
  }
}

async function remove(c) {
  postError.value = ''
  try {
    await deleteComment(props.albumId, c.id)
    await load()
  } catch (e) {
    postError.value = errMsg(e, '评论删除失败')
  }
}

onMounted(() => {
  load()
})
</script>

<style scoped>
.comments {
  margin-top: 24px;
  border-top: 1px solid var(--color-border);
  padding-top: 16px;
}

.comments-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 12px;
}

.comments-tip {
  padding: 24px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.comments-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
  font-size: var(--font-size-md);
}

.comment-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.post-box {
  margin-top: 16px;
  display: flex;
  gap: 8px;
}

.post-error {
  margin-top: 8px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.input {
  flex: 1;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
}

.input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.btn {
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.btn:hover {
  background-color: var(--color-surface-hover);
}

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.primary:hover {
  background-color: var(--color-primary-hover);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>

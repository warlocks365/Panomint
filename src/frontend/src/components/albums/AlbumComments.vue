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
      <li v-for="c in tree" :key="c.id" class="comment">
        <div class="comment-row">
          <span class="avatar">{{ initial(c.user_name) }}</span>
          <div class="comment-main">
            <div class="comment-head">
              <span class="comment-user">{{ c.user_name || '未知用户' }}</span>
              <span class="comment-time">{{ formatTime(c.created_at) }}</span>
            </div>
            <p class="comment-content">{{ c.content }}</p>
            <div class="comment-ops">
              <button class="op-btn" @click="toggleReplyBox(c.id)">回复</button>
              <button
                v-if="c.replies.length"
                class="op-btn"
                @click="toggleReplies(c.id)"
              >
                {{ expanded.has(c.id) ? '收起回复' : `展开 ${c.replies.length} 条回复` }}
              </button>
              <button v-if="isMine(c)" class="op-btn danger" @click="remove(c)">删除</button>
            </div>

            <div v-if="replyBoxFor === c.id" class="reply-box">
              <input
                v-model.trim="replyContent"
                class="input"
                type="text"
                maxlength="500"
                placeholder="回复这条评论…"
                @keyup.enter="submitReply(c.id)"
              />
              <button class="btn primary sm" :disabled="!replyContent || posting" @click="submitReply(c.id)">
                发送
              </button>
              <button class="btn sm" @click="replyBoxFor = ''">取消</button>
            </div>

            <ul v-if="c.replies.length && expanded.has(c.id)" class="reply-list">
              <li v-for="r in c.replies" :key="r.id" class="comment">
                <div class="comment-row">
                  <span class="avatar sm">{{ initial(r.user_name) }}</span>
                  <div class="comment-main">
                    <div class="comment-head">
                      <span class="comment-user">{{ r.user_name || '未知用户' }}</span>
                      <span class="comment-time">{{ formatTime(r.created_at) }}</span>
                    </div>
                    <p class="comment-content">{{ r.content }}</p>
                    <div class="comment-ops">
                      <button v-if="isMine(r)" class="op-btn danger" @click="remove(r)">删除</button>
                    </div>
                  </div>
                </div>
              </li>
            </ul>
          </div>
        </div>
      </li>
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
import { computed, onMounted, reactive, ref } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { createComment, deleteComment, errMsg, listComments } from './albumApi'

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

function isMine(c) {
  return myUserId.value && c.user_id === myUserId.value
}

function initial(name) {
  return (name || '?').trim().charAt(0).toUpperCase()
}

function formatTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

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

onMounted(async () => {
  if (!auth.user) {
    try {
      await auth.fetchMe()
    } catch (e) {
      // 用户信息获取失败不影响评论浏览，仅隐藏"删除"按钮
    }
  }
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

.comment-list,
.reply-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.comment {
  padding: 10px 0;
}

.reply-list {
  margin-top: 8px;
  padding-left: 8px;
  border-left: 2px solid var(--color-border);
}

.comment-row {
  display: flex;
  gap: 10px;
}

.avatar {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--font-size-md);
  color: #fff;
  background-color: var(--color-primary);
}

.avatar.sm {
  width: 26px;
  height: 26px;
  font-size: var(--font-size-sm);
}

.comment-main {
  flex: 1;
  min-width: 0;
}

.comment-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.comment-user {
  font-size: var(--font-size-md);
  font-weight: 600;
  color: var(--color-text-primary);
}

.comment-time {
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}

.comment-content {
  margin-top: 2px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  word-break: break-word;
}

.comment-ops {
  margin-top: 4px;
  display: flex;
  gap: 12px;
}

.op-btn {
  border: none;
  background: none;
  padding: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  cursor: pointer;
}

.op-btn:hover {
  color: var(--color-primary);
}

.op-btn.danger:hover {
  color: var(--color-danger);
}

.reply-box {
  margin-top: 8px;
  display: flex;
  gap: 8px;
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

.btn.sm {
  padding: 6px 12px;
  font-size: var(--font-size-sm);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>

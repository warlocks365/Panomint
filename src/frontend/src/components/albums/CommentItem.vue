<template>
  <li class="comment">
    <div class="comment-row">
      <span class="avatar">{{ initial(comment.user_name) }}</span>
      <div class="comment-main">
        <div class="comment-head">
          <span class="comment-user">{{ comment.user_name || '未知用户' }}</span>
          <span class="comment-time">{{ formatTime(comment.created_at) }}</span>
        </div>
        <p class="comment-content">{{ comment.content }}</p>
        <div class="comment-ops">
          <button class="op-btn" @click="$emit('toggle-reply-box', comment.id)">回复</button>
          <button
            v-if="comment.replies.length"
            class="op-btn"
            @click="$emit('toggle-replies', comment.id)"
          >
            {{ expanded ? '收起回复' : `展开 ${comment.replies.length} 条回复` }}
          </button>
          <button v-if="isMine" class="op-btn danger" @click="$emit('remove', comment)">删除</button>
        </div>

        <div v-if="replyBoxOpen" class="reply-box">
          <input
            :value="replyContent"
            class="input"
            type="text"
            maxlength="500"
            placeholder="回复这条评论…"
            @input="$emit('update:replyContent', $event.target.value)"
            @keyup.enter="$emit('submit-reply', comment.id)"
          />
          <button class="btn primary sm" :disabled="!replyContent || posting" @click="$emit('submit-reply', comment.id)">
            发送
          </button>
          <button class="btn sm" @click="$emit('toggle-reply-box', comment.id)">取消</button>
        </div>

        <ul v-if="comment.replies.length && expanded" class="reply-list">
          <li v-for="r in comment.replies" :key="r.id" class="comment">
            <div class="comment-row">
              <span class="avatar sm">{{ initial(r.user_name) }}</span>
              <div class="comment-main">
                <div class="comment-head">
                  <span class="comment-user">{{ r.user_name || '未知用户' }}</span>
                  <span class="comment-time">{{ formatTime(r.created_at) }}</span>
                </div>
                <p class="comment-content">{{ r.content }}</p>
                <div class="comment-ops">
                  <button v-if="isMineId(r)" class="op-btn danger" @click="$emit('remove', r)">删除</button>
                </div>
              </div>
            </div>
          </li>
        </ul>
      </div>
    </div>
  </li>
</template>

<script setup>
// 单条评论（含一层回复列表与回复框）——从 AlbumComments 抽出（Job000058-5）。
// 回复框/展开态的"全局唯一"仍由父级持有（单一打开语义不变），本组件纯展示+转发事件。
import { computed } from 'vue'

const props = defineProps({
  comment: { type: Object, required: true },
  expanded: { type: Boolean, default: false },
  replyBoxOpen: { type: Boolean, default: false },
  replyContent: { type: String, default: '' },
  posting: { type: Boolean, default: false },
  myUserId: { type: [String, Number], default: '' }
})
defineEmits([
  'toggle-replies',
  'toggle-reply-box',
  'update:replyContent',
  'submit-reply',
  'remove'
])

const isMine = computed(() => props.myUserId && props.comment.user_id === props.myUserId)
function isMineId(r) {
  return props.myUserId && r.user_id === props.myUserId
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
</script>

<style scoped>
.comment {
  padding: 10px 0;
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

.reply-list {
  list-style: none;
  margin: 8px 0 0;
  padding-left: 8px;
  border-left: 2px solid var(--color-border);
}

.reply-box {
  margin-top: 8px;
  display: flex;
  gap: 8px;
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

<template>
  <div class="dlg-mask" @click.self="$emit('close')">
    <div class="dlg" role="dialog" :aria-label="title">
      <h3 class="dlg-title">{{ title }}</h3>

      <!-- 新建 / 改名 -->
      <template v-if="mode === 'create' || mode === 'rename'">
        <label class="field">
          <span class="field-label">{{ mode === 'rename' ? '当前路径' : '父目录' }}</span>
          <input class="input" :value="basePath || '（根目录）'" type="text" disabled />
        </label>
        <label class="field">
          <span class="field-label">{{ mode === 'rename' ? '新路径' : '目录名' }}</span>
          <input
            v-model.trim="name"
            class="input"
            type="text"
            maxlength="120"
            :placeholder="mode === 'rename' ? '完整新路径，如 2024/旅行' : '新目录名'"
            @keyup.enter="submit"
          />
        </label>
        <p v-if="mode === 'rename'" class="hint">该目录及其全部子目录、所含媒体将移动到新路径</p>
      </template>

      <!-- 删除 -->
      <template v-else-if="mode === 'delete'">
        <p class="warn-text">确定删除目录「{{ targetPath }}」？</p>
        <p class="hint">该目录及其子目录下的全部媒体将移入回收站（可恢复），空目录登记一并清除。</p>
      </template>

      <!-- 权限 -->
      <template v-else-if="mode === 'grants'">
        <p class="hint">授权其他用户访问「{{ targetPath }}」及其全部子目录：</p>
        <div v-if="grants.length" class="grant-list">
          <div v-for="g in grants" :key="g.user_id" class="grant-row">
            <span class="grant-name">{{ userName(g.user_id) }}</span>
            <label class="grant-flag"><input v-model="g.read" type="checkbox" /> 可见</label>
            <label class="grant-flag"><input v-model="g.write" type="checkbox" /> 可写</label>
            <button type="button" class="btn mini" @click="removeGrant(g.user_id)">移除</button>
          </div>
        </div>
        <p v-else class="hint">暂无授权（目录仅自己可见）</p>
        <div v-if="candidates.length" class="add-row">
          <select v-model="addId" class="input" data-testid="grant-candidate">
            <option value="">选择用户…</option>
            <option v-for="u in candidates" :key="u.id" :value="u.id">{{ u.display_name || u.email }}</option>
          </select>
          <button type="button" class="btn" :disabled="!addId" @click="addGrant">添加</button>
        </div>
      </template>

      <p v-if="error" class="error">{{ error }}</p>

      <div class="dlg-actions">
        <button class="btn" type="button" @click="$emit('close')">取消</button>
        <button
          v-if="mode !== 'grants'"
          class="btn"
          :class="{ danger: mode === 'delete' }"
          type="button"
          :disabled="submitting || (mode !== 'delete' && !name)"
          @click="submit"
        >
          {{ submitting ? '处理中…' : confirmText }}
        </button>
        <button v-else class="btn primary" type="button" :disabled="submitting" @click="saveGrants">
          {{ submitting ? '保存中…' : '保存授权' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { createFolder, deleteFolder, renameFolder, setFolderGrants } from '../../api/folders'

// 目录管理四合一对话框（Job000069）：create/rename/delete/grants。
const props = defineProps({
  mode: { type: String, required: true },
  // rename/delete/grants 的目标目录路径；create 时为父目录（'' = 根）
  targetPath: { type: String, default: '' },
  initialGrants: { type: Array, default: () => [] },
  users: { type: Array, default: () => [] }
})
const emit = defineEmits(['close', 'done'])

const titleMap = { create: '新建文件夹', rename: '重命名/移动目录', delete: '删除目录', grants: '目录权限' }
const confirmMap = { create: '创建', rename: '移动', delete: '确认删除' }
const title = computed(() => titleMap[props.mode])
const confirmText = computed(() => confirmMap[props.mode])

const basePath = computed(() => props.targetPath)
const name = ref('')
const grants = ref(props.initialGrants.map((g) => ({ ...g })))
const addId = ref('')
const submitting = ref(false)
const error = ref('')

// 授权候选 = 全量用户 - 已授权（不含自己；自己=owner 恒有权）。
const candidates = computed(() =>
  props.users.filter((u) => !grants.value.some((g) => g.user_id === u.id))
)

function userName(id) {
  const u = props.users.find((x) => x.id === id)
  return u ? u.display_name || u.email : id.slice(0, 8)
}

function fullPath() {
  return basePath.value ? basePath.value + '/' + name.value : name.value
}

async function submit() {
  if (submitting.value) return
  submitting.value = true
  error.value = ''
  try {
    if (props.mode === 'create') {
      await createFolder(fullPath())
    } else if (props.mode === 'rename') {
      await renameFolder(props.targetPath, fullPath() || name.value)
    } else if (props.mode === 'delete') {
      await deleteFolder(props.targetPath)
    }
    emit('done')
  } catch (e) {
    error.value = e.response?.data?.error?.message || '操作失败'
  } finally {
    submitting.value = false
  }
}

function addGrant() {
  if (!addId.value) return
  grants.value.push({ user_id: addId.value, read: true, write: false })
  addId.value = ''
}

function removeGrant(id) {
  grants.value = grants.value.filter((g) => g.user_id !== id)
}

async function saveGrants() {
  if (submitting.value) return
  submitting.value = true
  error.value = ''
  try {
    await setFolderGrants(
      props.targetPath,
      grants.value.filter((g) => g.read || g.write)
    )
    emit('done')
  } catch (e) {
    error.value = e.response?.data?.error?.message || '保存失败'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.4);
}

.dlg {
  width: 440px;
  max-width: calc(100vw - 32px);
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}

.dlg-title { font-size: var(--font-size-lg); color: var(--color-text-primary); margin-bottom: 16px; }
.field { display: block; margin-bottom: 12px; }
.field-label { display: block; font-size: var(--font-size-sm); color: var(--color-text-secondary); margin-bottom: 4px; }
.input {
  width: 100%;
  box-sizing: border-box;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
}
.input:disabled { color: var(--color-text-disabled); background-color: var(--color-surface-hover); }
.hint { font-size: var(--font-size-sm); color: var(--color-text-secondary); margin-bottom: 12px; }
.warn-text { font-size: var(--font-size-md); color: var(--color-text-primary); margin-bottom: 8px; }
.error { margin-bottom: 12px; font-size: var(--font-size-sm); color: var(--color-danger); }
.grant-list { margin-bottom: 12px; }
.grant-row { display: flex; align-items: center; gap: 10px; padding: 6px 0; border-bottom: 1px solid var(--color-border); }
.grant-name { flex: 1; font-size: var(--font-size-md); color: var(--color-text-primary); }
.grant-flag { display: flex; align-items: center; gap: 4px; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.add-row { display: flex; gap: 8px; }
.add-row .input { flex: 1; }
.dlg-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
.btn {
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}
.btn:hover { background-color: var(--color-surface-hover); }
.btn.primary { border-color: var(--color-primary); color: #fff; background-color: var(--color-primary); }
.btn.danger { border-color: var(--color-danger); color: #fff; background-color: var(--color-danger); }
.btn.mini { padding: 3px 10px; font-size: var(--font-size-sm); }
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>

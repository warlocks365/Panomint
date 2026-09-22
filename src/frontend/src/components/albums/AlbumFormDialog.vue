<template>
  <div class="dlg-mask" @click.self="$emit('cancel')">
    <div class="dlg" role="dialog" :aria-label="isEdit ? '编辑相册' : '新建相册'">
      <h3 class="dlg-title">{{ isEdit ? (criteriaEditable ? '编辑智能相册' : '重命名相册') : '新建相册' }}</h3>

      <label class="field">
        <span class="field-label">名称</span>
        <input v-model.trim="form.name" class="input" type="text" maxlength="60" placeholder="请输入相册名称" />
      </label>

      <label class="field">
        <span class="field-label">描述</span>
        <textarea v-model.trim="form.description" class="input textarea" rows="2" maxlength="200" placeholder="可选"></textarea>
      </label>

      <div v-if="!isEdit" class="field">
        <span class="field-label">类型</span>
        <div class="kind-row">
          <button
            v-for="k in [
              { v: 'normal', t: '普通相册' },
              { v: 'smart', t: '智能相册' }
            ]"
            :key="k.v"
            type="button"
            class="kind-btn"
            :class="{ active: form.kind === k.v }"
            @click="form.kind = k.v"
          >
            {{ k.t }}
          </button>
        </div>
        <p class="field-hint">智能相册按条件自动收录媒体，内容不可手动增删</p>
      </div>

      <template v-if="showCriteria">
        <div class="field">
          <span class="field-label">媒体类型</span>
          <select v-model="form.criteria.type" class="input">
            <option value="">全部类型</option>
            <option value="photo">照片</option>
            <option value="video">视频</option>
            <option value="360">360</option>
          </select>
        </div>

        <div class="field-row">
          <label class="field half">
            <span class="field-label">开始日期</span>
            <input v-model="form.criteria.date_from" class="input" type="date" />
          </label>
          <label class="field half">
            <span class="field-label">结束日期</span>
            <input v-model="form.criteria.date_to" class="input" type="date" />
          </label>
        </div>

        <label class="field">
          <span class="field-label">地点</span>
          <input v-model.trim="form.criteria.place" class="input" type="text" maxlength="60" placeholder="如：杭州（可选）" />
        </label>

        <label class="fav-row">
          <input v-model="form.criteria.favorites" type="checkbox" />
          <span>仅收录收藏的媒体</span>
        </label>

        <CriteriaDimensions
          v-model:folderPaths="form.criteria.folder_paths"
          v-model:tagIds="form.criteria.tag_ids"
          v-model:personIds="form.criteria.person_ids"
        />
      </template>

      <p v-if="error" class="dlg-error">{{ error }}</p>

      <div class="dlg-actions">
        <button class="btn" type="button" @click="$emit('cancel')">取消</button>
        <button class="btn primary" type="button" :disabled="submitting || !form.name" @click="submit">
          {{ submitting ? '提交中…' : isEdit ? '保存' : '创建' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { buildCriteria, createAlbum, errMsg, updateAlbum } from './albumApi'
import CriteriaDimensions from './CriteriaDimensions.vue'

const props = defineProps({
  // 传入则为编辑模式；criteriaEditable 控制是否可改条件（智能相册详情页用）
  album: { type: Object, default: null },
  criteriaEditable: { type: Boolean, default: false }
})
const emit = defineEmits(['cancel', 'saved'])

const isEdit = computed(() => !!props.album)

const form = reactive({
  name: props.album?.name || '',
  description: props.album?.description || '',
  kind: props.album?.kind === 'smart' ? 'smart' : 'normal',
  criteria: {
    type: props.album?.criteria?.type || '',
    date_from: props.album?.criteria?.date_from || '',
    date_to: props.album?.criteria?.date_to || '',
    place: props.album?.criteria?.place || '',
    favorites: !!props.album?.criteria?.favorites,
    folder_paths: [...(props.album?.criteria?.folder_paths || [])],
    tag_ids: [...(props.album?.criteria?.tag_ids || [])],
    person_ids: [...(props.album?.criteria?.person_ids || [])]
  }
})

const showCriteria = computed(() =>
  isEdit.value ? props.criteriaEditable && props.album?.kind === 'smart' : form.kind === 'smart'
)

const submitting = ref(false)
const error = ref('')

async function submit() {
  if (submitting.value || !form.name) return
  submitting.value = true
  error.value = ''
  try {
    if (isEdit.value) {
      const payload = { name: form.name, description: form.description }
      if (showCriteria.value) payload.criteria = buildCriteria(form.criteria)
      const saved = await updateAlbum(props.album.id, payload)
      emit('saved', saved)
    } else {
      const payload = {
        name: form.name,
        kind: form.kind,
        description: form.description || undefined
      }
      if (form.kind === 'smart') payload.criteria = buildCriteria(form.criteria)
      const saved = await createAlbum(payload)
      emit('saved', saved)
    }
  } catch (e) {
    error.value = errMsg(e, isEdit.value ? '保存失败' : '创建失败')
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
  width: 420px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  overflow-y: auto;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}

.dlg-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 16px;
}

.field {
  display: block;
  margin-bottom: 12px;
}

.field-row {
  display: flex;
  gap: 12px;
}

.field.half {
  flex: 1;
}

.field-label {
  display: block;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-bottom: 4px;
}

.field-hint {
  margin-top: 6px;
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}

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

.input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.textarea {
  resize: vertical;
}

.kind-row {
  display: flex;
  gap: 8px;
}

.kind-btn {
  flex: 1;
  padding: 8px 0;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.kind-btn.active {
  border-color: var(--color-primary);
  color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
}

.fav-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  cursor: pointer;
}

.dlg-error {
  margin-bottom: 12px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
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

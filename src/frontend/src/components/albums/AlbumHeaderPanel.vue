<template>
  <header ref="bannerEl" class="ah-banner">
    <img v-if="coverUrl" :src="coverUrl" :alt="album.name" class="ah-cover" />
    <div v-else class="ah-cover ah-cover--empty">
      <svg viewBox="0 0 24 24" width="40" height="40" fill="none">
        <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5" />
        <path d="M3 15l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        <circle cx="9" cy="9.5" r="1.6" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </div>
    <!-- bveil：底部 82% 雾面，承托标题与操作（取自 --color-bg 色相） -->
    <div class="ah-bveil" aria-hidden="true"></div>

    <div class="ah-content">
      <div class="ah-info">
        <div class="ah-title-row">
          <BackButton @click="emit('back')" />
          <template v-if="!editing">
            <h2 class="ah-name">{{ album.name }}</h2>
            <span v-if="album.kind === 'smart'" class="kind-tag"><i class="tag-dot"></i>智能</span>
            <button class="icon-btn" title="编辑名称与描述" @click="startEdit">
              <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
                <path d="M4 20h4l11-11-4-4L4 16v4z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
                <path d="M13.5 6.5l4 4" stroke="currentColor" stroke-width="1.6" />
              </svg>
            </button>
          </template>
          <template v-else>
            <input v-model.trim="editForm.name" class="input name-input" type="text" maxlength="60" placeholder="相册名称" />
          </template>
        </div>
        <template v-if="!editing">
          <p class="ah-desc">{{ album.description || '暂无描述' }}</p>
        </template>
        <template v-else>
          <textarea v-model.trim="editForm.description" class="input desc-input" rows="2" maxlength="200" placeholder="相册描述（可选）"></textarea>
          <div class="edit-actions">
            <button class="btn sm" @click="editing = false">取消</button>
            <button class="btn primary sm" :disabled="!editForm.name || saving" @click="saveEdit">
              {{ saving ? '保存中…' : '保存' }}
            </button>
          </div>
          <p v-if="saveError" class="save-error">{{ saveError }}</p>
        </template>
        <p class="ah-meta">{{ items.length }} 项</p>
      </div>

      <div class="ah-actions">
        <button class="btn" @click="emit('action', 'share')">分享</button>
        <button class="btn" @click="emit('action', 'shareManage')">分享管理</button>
        <button class="btn" :disabled="!items.length" @click="emit('action', 'coverPicker')">设置封面</button>
        <button v-if="album.kind === 'smart'" class="btn" @click="emit('action', 'criteria')">编辑条件</button>
        <button v-if="album.kind !== 'smart'" class="btn primary" @click="emit('action', 'picker')">添加媒体</button>
        <router-link v-if="album.kind !== 'smart'" class="btn" data-testid="album-upload" :to="`/upload?album=${album.id}&albumName=${encodeURIComponent(album.name)}`">上传照片</router-link>
      </div>
    </div>
  </header>
  <div v-if="album.kind === 'smart'" class="criteria-bar">
    <svg viewBox="0 0 24 24" width="14" height="14" fill="none">
      <path d="M4 6h16M7 12h10M10 18h4" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
    </svg>
    <span>{{ criteriaSummary }}</span>
    <span class="criteria-note">智能相册按条件自动收录，不支持手动增删</span>
  </div>
</template>
<script setup>
// AlbumDetailView 拆解（Job000087）：相册头部整体自持——
// 封面加载（coverId watch + loadThumbUrl）、名称/描述行内编辑状态机（editForm/saving/saveError）、
// 智能相册条件条、动作按钮组（点击 emit action 分派，宿主握各对话框开关）。
// 保存成功 emit saved({saved, form})，宿主负责合并 album 对象。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import gsap from 'gsap'
import { errMsg, updateAlbum } from '../albums/albumApi'
import { summarizeCriteria } from '../albums/criteriaSummary'
import { loadThumbUrl } from '../timeline/mediaLoader'
import BackButton from '../BackButton.vue'
const props = defineProps({
  album: { type: Object, required: true },
  items: { type: Array, default: () => [] }
})
// back（Job000102）：返回钮点击上抛，宿主握 goBack（useBackNavigation）——router 属视图职责
const emit = defineEmits(['action', 'saved', 'back'])
/* ---------------- 名称/描述行内编辑 ---------------- */
const editing = ref(false)
const editForm = ref({ name: '', description: '' })
const saving = ref(false)
const saveError = ref('')
// 宿主重新加载（切相册/媒体变更）会替换 album 对象——任何替换即退出编辑态（承原页语义）
watch(() => props.album, () => {
  editing.value = false
})
function startEdit() {
  editForm.value = { name: props.album.name, description: props.album.description || '' }
  saveError.value = ''
  editing.value = true
}
async function saveEdit() {
  if (!editForm.value.name || saving.value) return
  saving.value = true
  saveError.value = ''
  try {
    const saved = await updateAlbum(props.album.id, {
      name: editForm.value.name,
      description: editForm.value.description
    })
    editing.value = false
    emit('saved', { saved, form: { ...editForm.value } })
  } catch (e) {
    saveError.value = errMsg(e, '保存失败')
  } finally {
    saving.value = false
  }
}
/* ---------------- 智能相册条件摘要 ---------------- */
const criteriaSummary = computed(() => summarizeCriteria(props.album?.criteria))
/* ---------------- 封面加载（封面 id 变化即重取 lg 缩略图） ---------------- */
const coverUrl = ref('')
let alive = true
const coverId = computed(() => props.album?.cover_media_id || props.items[0]?.id || '')
watch(coverId, async (id) => {
  coverUrl.value = ''
  if (!id) return
  try {
    const u = await loadThumbUrl({ id }, 'lg')
    if (alive) coverUrl.value = u
  } catch (e) {
    if (alive) coverUrl.value = ''
  }
})
onBeforeUnmount(() => {
  alive = false
})
/* ---------------- GSAP banner-in（DESIGN.md §8）----------------
   Vue 原生 gsap.context + 生命周期清理（@gsap/react 为 React 专用绑定，禁用）；
   头条整体浮入 + 内容行 stagger；prefers-reduced-motion 全跳过 */
const bannerEl = ref(null)
let bannerCtx = null
onMounted(() => {
  const mm = gsap.matchMedia()
  mm.add('(prefers-reduced-motion: no-preference)', () => {
    bannerCtx = gsap.context(() => {
      gsap.from('.ah-banner', { y: 16, opacity: 0, duration: 0.55, ease: 'power2.out' })
      gsap.from('.ah-title-row, .ah-desc, .ah-meta, .ah-actions', {
        y: 10,
        opacity: 0,
        duration: 0.45,
        stagger: 0.06,
        delay: 0.15,
        ease: 'power2.out',
        clearProps: 'transform,opacity'
      })
    }, bannerEl.value)
  })
})
onBeforeUnmount(() => {
  if (bannerCtx) bannerCtx.revert()
})
</script>
<style scoped>
/* 封面头条（DESIGN.md 深化稿 VIEW.03-B）：大封面 banner + 底部雾面承托内容 */
.ah-banner {
  position: relative;
  height: 264px;
  border-radius: var(--radius-lg);
  overflow: hidden;
  background-color: var(--color-surface-hover);
  box-shadow: var(--shadow-card);
  margin-bottom: 16px;
}

.ah-cover {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.ah-cover--empty {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-disabled);
  background: linear-gradient(160deg, var(--color-surface), var(--color-surface-hover));
}

.ah-bveil {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 82%;
  background: linear-gradient(180deg, rgba(233, 228, 222, 0), rgba(233, 228, 222, 0.78) 42%, rgba(233, 228, 222, 0.96));
  pointer-events: none;
}

.ah-content {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  padding: 16px 20px;
}

.ah-info {
  flex: 1;
  min-width: 0;
}

.ah-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ah-name {
  font-size: 20px;
  letter-spacing: 0.04em;
  font-weight: 500;
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kind-tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2.5px 9px;
  border-radius: 999px;
  font-size: 11px;
  line-height: 1.5;
  color: var(--color-text-primary);
  background: rgba(244, 241, 237, 0.82);
  box-shadow: 0 1px 3px rgba(65, 64, 60, 0.08);
  flex-shrink: 0;
}

.tag-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background-color: var(--color-primary);
  flex-shrink: 0;
}

.icon-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-secondary);
  background-color: transparent;
  cursor: pointer;
  flex-shrink: 0;
}

.icon-btn:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}

.ah-desc {
  margin-top: 4px;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.ah-meta {
  margin-top: 6px;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
}

.ah-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
  flex-shrink: 0;
}

.name-input {
  max-width: 320px;
}

.desc-input {
  max-width: 480px;
  resize: vertical;
}

.edit-actions {
  display: flex;
  gap: 8px;
}

.save-error {
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.criteria-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 12px;
  margin-bottom: 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-primary-active-bg);
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.criteria-note {
  color: var(--color-text-secondary);
}

.input {
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

/* 响应式：<768px 头部内容纵向堆叠，操作钮左对齐换行 */
@media (max-width: 768px) {
  .ah-banner {
    height: 224px;
  }

  .ah-content {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }

  .ah-name {
    white-space: normal;
    font-size: 18px;
  }

  .ah-actions {
    justify-content: flex-start;
  }
}
</style>

<template>
  <div class="dlg-mask" @click.self="emit('close')">
    <div class="dlg" role="dialog" aria-label="挂载编辑" data-testid="storage-dialog">
      <h3 class="dlg-title">{{ mode === 'create' ? '新建挂载' : '编辑挂载' }}</h3>

      <label class="field">
        <span class="field-label">名称</span>
        <input v-model.trim="form.name" class="input" type="text" maxlength="120" data-testid="storage-dlg-name" />
      </label>

      <!-- 协议类型+连接字段+凭据字段组（Job000118 拆出，行数棘轮 O1） -->
      <MountConnFields :form="form" :mode="mode" />

      <label class="field">
        <span class="field-label">可见性</span>
        <select v-model="form.visibility" class="input" data-testid="storage-dlg-visibility">
          <option value="personal">个人（仅管理员与自己）</option>
          <option value="shared">共享（全部用户可用）</option>
        </select>
      </label>

      <!-- Job000118 导入落点：媒体库根下的语义目录。创建时默认 imports/<名称slug>，
           可改；创建后不可改（编辑态锁定）。空挂载目录创建后即在「文件夹」页签可见，
           并会被扫描导入覆盖。 -->
      <label class="field">
        <span class="field-label">导入到</span>
        <input v-model.trim="form.landingDir" class="input" type="text" :disabled="mode === 'edit'" placeholder="imports/我的相册" data-testid="storage-dlg-landing" @input="landingTouched = true" />
        <span v-if="mode === 'edit'" class="hint-inline">导入落点创建后不可更改</span>
        <span v-else-if="landingErr" class="hint-inline hint-inline--error" data-testid="storage-dlg-landing-err">{{ landingErr }}</span>
        <span v-else class="hint-inline">相对媒体库根；多级用 / 分隔；缺省 imports/&lt;名称&gt;</span>
      </label>

      <p v-if="mode === 'edit' && editing?.has_creds" class="hint">
        该挂载已设凭据：密码留 <code>********</code> 不变；输入新值整体覆盖；三项全空则清除凭据。
      </p>
      <p v-if="dialogError" class="msg msg--error" data-testid="storage-dlg-error">{{ dialogError }}</p>
      <div class="dlg-actions">
        <button class="btn" type="button" @click="emit('close')">取消</button>
        <button class="btn btn--primary" type="button" :disabled="submitting" data-testid="storage-submit" @click="submit">
          {{ submitting ? '保存中…' : mode === 'create' ? '创建' : '保存' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
// StorageTab 拆解（Job000085）：新建/编辑挂载对话框。
// 表单态/提交/凭据占位语义（编辑态已设凭据=留空不变/输入覆盖/全空清除）全部自持；
// 提交成功 emit saved(message)，宿主负责全局消息与列表刷新。凭据红线：密文不出端点，界面不回显。
import { computed, ref, watch } from 'vue'
import { errMessage } from '../../stores/auth'
import { createMount, patchMount } from '../../api/storage'
import MountConnFields from './MountConnFields.vue'

const props = defineProps({
  mode: { type: String, required: true }, // 'create' | 'edit'
  editing: { type: Object, default: null }
})
const emit = defineEmits(['close', 'saved'])

const submitting = ref(false)
const dialogError = ref('')
const form = ref(emptyForm())
// 落点自动跟随名称（用户手动编辑后停跟；编辑态恒锁）。
const landingTouched = ref(false)

const credMask = '********'

// slugify：与后端 slugifyName 同口径（小写、非字母数字压成连字符、去首尾连字符）。
function slugify(s) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

// 落点预校验（与后端 normalizeLandingDir 同口径；后端 400 BAD_LANDING_DIR 仍是权威闸门）。
const landingErr = computed(() => {
  if (props.mode === 'edit') return ''
  const segs = (form.value.landingDir || '').trim().replace(/^\/+|\/+$/g, '').split('/')
  if (segs.length === 1 && segs[0] === '') return '导入落点不能为空'
  for (const seg of segs) {
    if (!seg || seg === '.' || seg === '..') return `落点含非法路径段「${seg || '(空)'}」`
    if (seg.toLowerCase() === '@eadir') return `落点段「${seg}」为系统保留目录`
    if (!/^[a-z0-9][a-z0-9-]{0,63}$/.test(seg)) return `落点段「${seg}」须为字母/数字开头的小写字母·数字·连字符（≤64 位）`
  }
  if (segs[0] === '_imports') return '落点前缀 _imports 为系统保留，请改用 imports/<名称>'
  return ''
})

watch(
  () => form.value.name,
  (n) => {
    if (props.mode === 'edit' || landingTouched.value) return
    const slug = slugify(n || '')
    form.value.landingDir = 'imports/' + (slug || 'import')
  }
)

if (props.mode === 'edit' && props.editing) {
  const m = props.editing
  const c = m.conn || {}
  form.value = {
    name: m.name,
    type: m.type,
    url: c.url || '',
    host: c.host || '',
    share: c.share || '',
    export: c.export || '',
    port: c.port || null,
    credsUser: '',
    credsPass: '',
    credsDomain: '',
    visibility: m.visibility || 'personal',
    landingDir: m.landing_dir || ''
  }
}

function emptyForm() {
  return {
    name: '',
    type: 'webdav',
    url: '',
    host: '',
    share: '',
    export: '',
    port: null,
    credsUser: '',
    credsPass: '',
    credsDomain: '',
    visibility: 'personal',
    landingDir: 'imports/import'
  }
}

function buildConn() {
  const f = form.value
  if (f.type === 'webdav') return { url: f.url }
  if (f.type === 'smb') {
    const c = { host: f.host, share: f.share }
    if (f.port) c.port = f.port
    return c
  }
  return { host: f.host, export: f.export }
}

async function submit() {
  if (submitting.value) return
  if (landingErr.value) {
    dialogError.value = landingErr.value
    return
  }
  submitting.value = true
  dialogError.value = ''
  const f = form.value
  try {
    if (props.mode === 'create') {
      const payload = { name: f.name, type: f.type, conn: buildConn(), visibility: f.visibility }
      if (f.type !== 'nfs') {
        payload.creds_user = f.credsUser
        payload.creds_pass = f.credsPass
        payload.creds_domain = f.credsDomain
      }
      // Job000118：导入落点（缺省 imports/<slug> 时仍显式回传，后端校验+兜底双保险）。
      payload.landing_dir = (f.landingDir || '').trim().replace(/^\/+|\/+$/g, '')
      await createMount(payload)
      emit('saved', '已创建：落点目录已就绪并可在「文件夹」中查看，worker 执行器将在下一个对账周期（约 10 秒）自动挂载')
    } else {
      const patch = { name: f.name, conn: buildConn(), visibility: f.visibility }
      if (f.type !== 'nfs') {
        const any = f.credsUser || f.credsPass || f.credsDomain
        if (any) {
          patch.creds_user = f.credsUser
          patch.creds_pass = f.credsPass
          patch.creds_domain = f.credsDomain
        } else {
          patch.creds_user = ''
          patch.creds_pass = props.editing?.has_creds ? credMask : ''
          patch.creds_domain = ''
        }
      }
      await patchMount(props.editing.id, patch)
      emit('saved', '已保存')
    }
  } catch (e) {
    dialogError.value = errMessage(e, '保存失败')
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
  width: 460px;
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
  margin: 0 0 16px;
}
.field {
  display: block;
  margin-bottom: 12px;
}
.field-label {
  display: block;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-bottom: 4px;
}
.hint-inline {
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
  margin-top: 4px;
}
.hint-inline--error {
  color: var(--color-danger);
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
.input:disabled {
  color: var(--color-text-disabled);
  background-color: var(--color-surface-hover);
}
.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 8px;
}
.hint {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin: 0 0 14px;
}
.hint code {
  background: var(--color-surface-hover);
  padding: 1px 6px;
  border-radius: var(--radius-sm);
}
.msg {
  margin: 0 0 8px;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
}

.btn {
  padding: 6px 14px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
}
.btn:hover {
  background: var(--color-surface-hover);
}
.btn--primary {
  background: var(--color-primary);
  border-color: var(--color-primary);
  color: var(--color-surface);
}
.btn--primary:hover {
  background: var(--color-primary-hover);
}
</style>

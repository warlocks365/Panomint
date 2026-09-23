<template>
  <div class="dlg-mask" @click.self="emit('close')">
    <div class="dlg" role="dialog" aria-label="挂载编辑" data-testid="storage-dialog">
      <h3 class="dlg-title">{{ mode === 'create' ? '新建挂载' : '编辑挂载' }}</h3>

      <label class="field">
        <span class="field-label">名称</span>
        <input v-model.trim="form.name" class="input" type="text" maxlength="120" data-testid="storage-dlg-name" />
      </label>

      <label class="field">
        <span class="field-label">协议类型</span>
        <select v-model="form.type" class="input" :disabled="mode === 'edit'" data-testid="storage-dlg-type">
          <option value="webdav">WebDAV</option>
          <option value="smb">SMB</option>
          <option value="nfs">NFS</option>
        </select>
        <span v-if="mode === 'edit'" class="hint-inline">类型创建后不可更改（连接配置结构不同）</span>
      </label>

      <template v-if="form.type === 'webdav'">
        <label class="field">
          <span class="field-label">服务器 URL</span>
          <input v-model.trim="form.url" class="input" type="text" placeholder="https://example.com/dav" data-testid="storage-dlg-url" />
        </label>
      </template>
      <template v-else-if="form.type === 'smb'">
        <label class="field">
          <span class="field-label">主机</span>
          <input v-model.trim="form.host" class="input" type="text" placeholder="192.168.1.10" data-testid="storage-dlg-host" />
        </label>
        <label class="field">
          <span class="field-label">共享名</span>
          <input v-model.trim="form.share" class="input" type="text" placeholder="photos" data-testid="storage-dlg-share" />
        </label>
        <label class="field">
          <span class="field-label">端口（可选，默认 445）</span>
          <input v-model.number="form.port" class="input" type="number" min="0" max="65535" data-testid="storage-dlg-port" />
        </label>
      </template>
      <template v-else>
        <label class="field">
          <span class="field-label">主机</span>
          <input v-model.trim="form.host" class="input" type="text" placeholder="192.168.1.10" data-testid="storage-dlg-host" />
        </label>
        <label class="field">
          <span class="field-label">导出路径</span>
          <input v-model.trim="form.export" class="input" type="text" placeholder="/srv/photos" data-testid="storage-dlg-export" />
        </label>
      </template>
      <template v-if="form.type !== 'nfs'">
        <label class="field">
          <span class="field-label">用户名（可选，匿名留空）</span>
          <input v-model="form.credsUser" class="input" type="text" :placeholder="userPlaceholder" data-testid="storage-dlg-user" autocomplete="off" />
        </label>
        <label class="field">
          <span class="field-label">密码</span>
          <input v-model="form.credsPass" class="input" type="password" :placeholder="passPlaceholder" data-testid="storage-dlg-pass" autocomplete="new-password" />
        </label>
        <label v-if="form.type === 'smb'" class="field">
          <span class="field-label">域（可选）</span>
          <input v-model="form.credsDomain" class="input" type="text" data-testid="storage-dlg-domain" autocomplete="off" />
        </label>
      </template>

      <label class="field">
        <span class="field-label">可见性</span>
        <select v-model="form.visibility" class="input" data-testid="storage-dlg-visibility">
          <option value="personal">个人（仅管理员与自己）</option>
          <option value="shared">共享（全部用户可用）</option>
        </select>
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
import { computed, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { createMount, patchMount } from '../../api/storage'

const props = defineProps({
  mode: { type: String, required: true }, // 'create' | 'edit'
  editing: { type: Object, default: null }
})
const emit = defineEmits(['close', 'saved'])

const submitting = ref(false)
const dialogError = ref('')
const form = ref(emptyForm())

const credMask = '********'
const userPlaceholder = computed(() =>
  props.mode === 'edit' && props.editing?.has_creds ? '已设置（留空不变）' : '可选'
)
const passPlaceholder = computed(() =>
  props.mode === 'edit' && props.editing?.has_creds ? credMask + '（不改动）' : '可选'
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
    visibility: m.visibility || 'personal'
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
    visibility: 'personal'
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
      await createMount(payload)
      emit('saved', '已创建，worker 执行器将在下一个对账周期（约 10 秒）自动挂载')
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

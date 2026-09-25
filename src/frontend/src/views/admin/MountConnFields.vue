<template>
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
</template>

<script setup>
// Job000118 拆分（frontend_org O1 棘轮）：协议类型选择 + 按类型连接字段 + 凭据字段组。
// form 对象引用共享（表单场景直写字段，data-testid 与拆分前逐字一致）；
// 凭据占位语义（编辑态已设凭据=留空不变/输入覆盖/全空清除）的文案源自持。
import { computed } from 'vue'

const props = defineProps({
  form: { type: Object, required: true },
  mode: { type: String, required: true } // 'create' | 'edit'
})

const credMask = '********'
const userPlaceholder = computed(() =>
  props.mode === 'edit' ? '已设置（留空不变）' : '可选'
)
const passPlaceholder = computed(() =>
  props.mode === 'edit' ? credMask + '（不改动）' : '可选'
)
</script>

<style scoped>
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
</style>

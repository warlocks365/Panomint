<template>
  <!-- 单一根节点是硬约束：AdminView 用 v-show 切换页签，Vue3 的 v-show 落在多根
       fragment 上会失效（指令无法挂到任何单一根），导致本页签内容穿透到所有页签
       （2026-09-25 实测踩坑：存储页签在任意页签下都渲染）。所有子面板必须包在这一层里。 -->
  <div class="storage-tab">
    <!-- Job000113 / R1-b 扫描导入：把挂载目录里已有的照片/视频入库（归当前账号） -->
    <StorageScanPanel />

    <section class="card">
      <div class="card-head">
        <h2 class="card-title">网络挂载</h2>
        <div class="head-actions">
          <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="storage-reload" @click="load()">
            {{ loading ? '加载中…' : '刷新' }}
          </button>
          <button class="btn btn--primary" type="button" data-testid="storage-create" @click="openDialog('create')">
            新建挂载
          </button>
        </div>
      </div>

      <p class="hint">
        挂载远程存储（WebDAV / SMB / NFS）并增量导入媒体库：worker 侧执行器自动对账，
        导入文件落在创建时指定的语义目录（默认 <code>imports/&lt;名称&gt;/</code>）——
        目录在「文件夹」页签立即可见、可被扫描，创建后不可改。
        凭据经服务端 AES-256-GCM 加密存储，界面不回显。
      </p>

      <p v-if="err" class="msg msg--error" data-testid="storage-error">{{ err }}</p>
      <p v-if="msg" class="msg msg--ok" data-testid="storage-msg">{{ msg }}</p>
      <p v-if="forbidden" class="msg msg--error">当前账号缺少管理权限，无法管理挂载。</p>

      <p v-if="!loading && !err && mounts.length === 0" class="muted-empty" data-testid="storage-empty">
        暂无挂载
      </p>

      <StorageMountList :mounts="mounts" @edit="openDialog('edit', $event)" @delete="remove" />

      <!-- 新建 / 编辑对话框（表单自持，提交成功上报 saved） -->
      <StorageMountDialog
        v-if="dialogMode"
        :mode="dialogMode"
        :editing="editing"
        @close="closeDialog"
        @saved="onSaved"
      />
    </section>
  </div>
</template>

<script setup>
// Job000085 拆解：列表行 → StorageMountList（行内交互态自持）；
// 新建/编辑对话框 → StorageMountDialog（表单/凭据语义自持）。
// 本组件保留：数据加载与 15s 轻量轮询（document.hidden 与对话框打开时暂停，避让共享限流桶）、
// 全局消息、删除执行、对话框开关分派。
// 状态机取值见 cmd/storagectl：''=未挂载 / online=在线 / error=错误
//（v2 探针落地后增补 offline/degraded，前端先行识别）。
import { onMounted, onUnmounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { deleteMount, listMounts } from '../../api/storage'
import StorageMountList from './StorageMountList.vue'
import StorageMountDialog from './StorageMountDialog.vue'
import StorageScanPanel from './StorageScanPanel.vue'

const mounts = ref([])
const loading = ref(false)
const err = ref('')
const msg = ref('')
const forbidden = ref(false)

const dialogMode = ref('') // '' | 'create' | 'edit'
const editing = ref(null)

let pollTimer = null

async function load(quiet = false) {
  if (!quiet) loading.value = true
  err.value = ''
  try {
    mounts.value = await listMounts()
  } catch (e) {
    if (e?.response?.status === 403) {
      forbidden.value = true
    } else {
      err.value = errMessage(e, '加载挂载列表失败')
    }
  } finally {
    loading.value = false
  }
}

function openDialog(mode, m) {
  dialogMode.value = mode
  editing.value = mode === 'edit' ? m : null
}

function closeDialog() {
  dialogMode.value = ''
  editing.value = null
}

async function onSaved(message) {
  msg.value = message
  closeDialog()
  await load(true)
}

async function remove(m) {
  msg.value = ''
  err.value = ''
  try {
    await deleteMount(m.id)
    msg.value = '已删除，挂载卸载与媒体清理由执行器对账完成'
    await load(true)
  } catch (e) {
    err.value = errMessage(e, '删除失败')
  }
}

onMounted(() => {
  load()
  // 状态由 worker 执行器每 10s 对账改写，列表轻量轮询跟随（避开共享限流桶的 15s 节拍）
  pollTimer = setInterval(() => {
    if (!document.hidden && !dialogMode.value) load(true)
  }, 15000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
/* 页签根：两块面板（扫描导入/网络挂载）纵向排布留缝 */
.storage-tab {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 20px;
}
.card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.card-title {
  margin: 0;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}
.head-actions {
  display: flex;
  gap: 8px;
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
.msg--ok {
  color: var(--color-success);
}
.muted-empty {
  padding: 32px 0;
  text-align: center;
  color: var(--color-text-secondary);
  font-size: var(--font-size-md);
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
.btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.btn--primary {
  background: var(--color-primary);
  border-color: var(--color-primary);
  color: var(--color-surface);
}
.btn--primary:hover {
  background: var(--color-primary-hover);
}
.btn--ghost {
  border-color: transparent;
  color: var(--color-primary);
}
</style>

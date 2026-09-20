<template>
  <div class="table-wrap">
    <table class="table" data-testid="users-table">
      <thead>
        <tr>
          <th>邮箱</th>
          <th>昵称</th>
          <th>角色</th>
          <th>状态</th>
          <th>二次验证</th>
          <th class="col-actions">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="u in users" :key="u.id" :data-testid="`user-row-${u.email}`">
          <td>{{ u.email }}</td>
          <td>{{ u.display_name || '—' }}</td>
          <td>
            <select
              :value="u.role"
              :data-testid="`user-role-${u.email}`"
              :disabled="u.id === meId"
              @change="$emit('role-change', u, $event.target.value)"
            >
              <option v-for="r in roleOptions" :key="r" :value="r">{{ r }}</option>
            </select>
          </td>
          <td>
            <span class="state" :class="u.status === 'active' ? 'state--ok' : 'state--off'">
              {{ u.status === 'active' ? '正常' : '已禁用' }}
            </span>
          </td>
          <td>{{ u.mfa_enabled ? '已启用' : u.mfa_pending ? '待确认' : '未启用' }}</td>
          <td class="col-actions">
            <button
              class="btn btn--mini"
              type="button"
              :disabled="u.status !== 'active'"
              :data-testid="`user-toggle-${u.email}`"
              @click="$emit('toggle-status', u)"
            >
              {{ u.status === 'active' ? '禁用' : '启用' }}
            </button>
            <button
              class="btn btn--mini"
              type="button"
              :data-testid="`user-reset-${u.email}`"
              @click="$emit('reset-password', u)"
            >
              重置密码
            </button>
            <button
              class="btn btn--mini btn--danger"
              type="button"
              :class="{ 'btn--confirm': confirmDeleteId === u.id }"
              :data-testid="`user-delete-${u.email}`"
              @click="$emit('delete', u)"
            >
              {{ confirmDeleteId === u.id ? '再点一次确认删除' : '删除' }}
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup>
defineProps({
  users: { type: Array, default: () => [] },
  roleOptions: { type: Array, default: () => [] },
  meId: { type: String, default: '' },
  confirmDeleteId: { type: String, default: '' }
})
defineEmits(['role-change', 'toggle-status', 'reset-password', 'delete'])
</script>

<style scoped>
.table-wrap {
  overflow-x: auto;
}
.table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-md);
}
.table th,
.table td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--color-border);
  color: var(--color-text-primary);
  white-space: nowrap;
}
.table th {
  color: var(--color-text-secondary);
  font-weight: 500;
}
.col-actions {
  display: flex;
  gap: 6px;
}
.state {
  font-size: var(--font-size-sm);
}
.state--ok {
  color: var(--color-success);
}
.state--off {
  color: var(--color-text-disabled);
}
select {
  padding: 6px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background: var(--color-surface);
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
.btn--mini {
  padding: 4px 10px;
  font-size: var(--font-size-sm);
}
.btn--danger {
  color: var(--color-danger);
  border-color: var(--color-danger);
}
.btn--confirm {
  background: var(--color-danger);
  color: var(--color-surface);
}
</style>

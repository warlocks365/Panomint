<template>
  <div>
    <template v-for="(b, i) in blocks" :key="i">
      <h3 v-if="b.t === 'h3'" class="mb-h3" v-html="fmt(b.x)"></h3>
      <p v-else-if="b.t === 'p'" class="mb-p" v-html="fmt(b.x)"></p>
      <p v-else-if="b.t === 'note'" class="mb-note" v-html="fmt(b.x)"></p>
      <p v-else-if="b.t === 'warn'" class="mb-warn" v-html="fmt(b.x)"></p>
      <pre v-else-if="b.t === 'code'" class="mb-code"><code>{{ b.x }}</code></pre>
      <ul v-else-if="b.t === 'ul'" class="mb-list">
        <li v-for="(it, j) in b.items" :key="j" v-html="fmt(it)"></li>
      </ul>
      <ol v-else-if="b.t === 'steps'" class="mb-steps">
        <li v-for="(it, j) in b.items" :key="j" v-html="fmt(it)"></li>
      </ol>
      <div v-else-if="b.t === 'table'" class="mb-table-wrap">
        <table class="mb-table">
          <thead>
            <tr><th v-for="(h, k) in b.head" :key="k" v-html="fmt(h)"></th></tr>
          </thead>
          <tbody>
            <tr v-for="(r, j) in b.rows" :key="j">
              <td v-for="(c, k) in r" :key="k" v-html="fmt(c)"></td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </div>
</template>

<script setup>
// 块渲染器：usage 与 deep 两处复用。
// 行内格式约定（先整串转义再做替换，杜绝注入）：`xx` -> <code>，**xx** -> <strong>
defineProps({ blocks: { type: Array, required: true } })

function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
function fmt(s) {
  if (s == null) return ''
  return esc(s)
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
}
</script>

<style scoped>
.mb-h3 {
  margin: 20px 0 8px;
  font-size: 14.5px;
  font-weight: 500;
  letter-spacing: 0.03em;
  color: var(--color-text-primary);
}
.mb-p {
  margin: 8px 0;
  line-height: 1.9;
  color: var(--color-text-primary);
}
.mb-note,
.mb-warn {
  margin: 10px 0;
  padding: 9px 12px;
  border-radius: 0 9px 9px 0;
  line-height: 1.7;
  font-size: var(--font-size-sm);
}
/* 提示条：语义色左线 + 浅底（DESIGN.md 深化稿 VIEW.14） */
.mb-note {
  border-left: 2px solid var(--color-success);
  background: rgba(110, 138, 114, 0.08);
  color: var(--color-text-secondary);
}
.mb-warn {
  border-left: 2px solid var(--stat-pano-photo);
  background: rgba(196, 165, 122, 0.1);
  color: var(--color-text-secondary);
}
.mb-code {
  margin: 10px 0;
  padding: 12px 16px;
  background: #3a4652;
  color: #d9e0e6;
  border-radius: 10px;
  overflow-x: auto;
  font-family: var(--font-mono);
  font-size: 11.5px;
  line-height: 1.7;
}
.mb-list,
.mb-steps {
  margin: 8px 0;
  line-height: 1.8;
  color: var(--color-text-primary);
}
.mb-list {
  padding-left: 22px;
}
/* 步骤列表：mono 计数胶囊（DESIGN.md 深化稿 VIEW.14） */
.mb-steps {
  list-style: none;
  counter-reset: mbst;
  padding-left: 0;
}
.mb-steps li {
  counter-increment: mbst;
  position: relative;
  padding: 0 0 10px 34px;
}
.mb-steps li::before {
  content: counter(mbst, decimal-leading-zero);
  position: absolute;
  left: 0;
  top: 1px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--color-primary);
  background: rgba(74, 90, 106, 0.1);
  border-radius: 6px;
  padding: 2px 6px;
}
.mb-table-wrap {
  margin: 10px 0;
  overflow-x: auto;
}
.mb-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-sm);
}
.mb-table th,
.mb-table td {
  border: 1px solid var(--color-border);
  padding: 7px 10px;
  text-align: left;
  line-height: 1.6;
  vertical-align: top;
}
.mb-table th {
  background: var(--color-surface-hover);
  color: var(--color-text-secondary);
  font-weight: 500;
  white-space: nowrap;
}
.mb-table td {
  color: var(--color-text-primary);
}
.mb-p :deep(code),
.mb-list :deep(code),
.mb-steps :deep(code),
.mb-note :deep(code),
.mb-warn :deep(code),
.mb-table :deep(code) {
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(74, 90, 106, 0.08);
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--color-text-primary);
}
</style>

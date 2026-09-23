import { computed, reactive, ref } from 'vue'
import http from '../../api/http'
import { dialogs } from '../dialogs/dialogs'

// Job000066 批量操作执行器：选中集 + 七类操作统一实现（后端 /media/batch 单点语义）。
// Job000077 交互对话框化：prompt/confirm 全部走统一对话框宿主（可机器断言、行为一致）。
export function useBatchOps(onChanged) {
  const selected = reactive(new Set())
  const busy = ref(false)
  const lastResult = ref('')

  const count = computed(() => selected.size)
  const ids = () => Array.from(selected)

  function toggle(id) {
    if (selected.has(id)) selected.delete(id)
    else selected.add(id)
  }

  function clear() {
    selected.clear()
  }

  // Job000099 统一全选/反选：宿主传「当前可选全集」的 id 数组（虚拟滚动宿主=已加载集，
  // 地图宿主=簇内容物；全集语义由宿主决定， composable 不感知数据源）。
  function isAllSelected(allIds) {
    return allIds.length > 0 && allIds.every((id) => selected.has(id))
  }

  function toggleAll(allIds) {
    if (isAllSelected(allIds)) {
      selected.clear()
    } else {
      for (const id of allIds) selected.add(id)
    }
  }

  function invertAll(allIds) {
    const keep = new Set(allIds)
    const next = []
    for (const id of allIds) {
      if (!selected.has(id)) next.push(id)
    }
    for (const id of selected) {
      if (!keep.has(id)) next.push(id) // 全集外的选中项保留（防误清其他批次勾选项）
    }
    selected.clear()
    for (const id of next) selected.add(id)
  }

  async function run(op, params = {}, okText) {
    if (busy.value || selected.size === 0) return
    busy.value = true
    lastResult.value = ''
    try {
      const { data } = await http.post('/media/batch', { ids: ids(), op, ...params })
      const fail = (data?.failed || []).length
      lastResult.value = `${okText}：成功 ${data?.succeeded ?? 0} 项${fail ? `，失败 ${fail} 项` : ''}`
      // Job000099 统一约定：操作成功 → 清空选择集 + 通知宿主刷新；失败 → 保留选择集（可修正后重试）
      selected.clear()
      onChanged?.()
    } catch (e) {
      lastResult.value = '操作失败：' + (e.response?.data?.error?.message || e.message)
    } finally {
      busy.value = false
    }
  }

  async function onDelete() {
    const ok = await dialogs.confirm({
      title: '删除',
      text: `删除选中的 ${selected.size} 项？删除进入回收站，可恢复。`,
      confirmText: '删除',
      danger: true
    })
    if (!ok) return
    await run('delete', {}, '已移入回收站')
  }

  async function onMove() {
    // Job000100 目标选择器：目录（改归属）或相册（加入成员，虚拟集合原目录保留）二选一
    const target = await dialogs.pickTarget({ title: `移动 ${selected.size} 项到`, mode: 'move', count: selected.size })
    if (!target) return
    if (target.type === 'album') {
      await run('move', { album_id: target.id }, '已加入相册')
    } else {
      await run('move', { folder_path: target.path }, '已移动')
    }
  }

  async function onCopy() {
    // Job000100 目标选择器：目录与相册可同时（副本落目标目录并加入相册）；
    // 取消 = 不做。未选目标时后端保持旧行为（副本继承源目录）。
    const target = await dialogs.pickTarget({ title: `复制 ${selected.size} 项到`, mode: 'copy', count: selected.size })
    if (!target) return
    const params = {}
    let okText = '已复制'
    if (target.type === 'album') {
      params.album_id = target.id
      okText = '已复制并加入相册'
    } else {
      params.folder_path = target.path
    }
    await run('copy', params, okText)
  }

  async function onShareSpace() {
    const ok = await dialogs.confirm({
      title: '共享空间',
      text: `将选中的 ${selected.size} 项移入共享空间？`
    })
    if (!ok) return
    await run('share_space', {}, '已共享')
  }

  async function onTags() {
    const names = await dialogs.prompt({
      title: '编辑标签',
      label: '标签名（多个用逗号分隔）。前缀 + 添加 / - 移除，如 "+海边,家庭" 或 "-临时"'
    })
    if (names === null) return
    const raw = names.trim()
    if (!raw) return
    const add = !raw.startsWith('-')
    const list = raw.replace(/^[+-]/, '').split(/[,，]/).map((s) => s.trim()).filter(Boolean)
    if (!list.length) return
    // 标签名 → id：查全量标签做映射（tag 不存在时创建，语义与 TagsView 一致）
    const { data } = await http.get('/tags')
    const all = Array.isArray(data) ? data : data?.tags || []
    const byName = new Map(all.map((t) => [t.name, t.id]))
    const tagIDs = []
    for (const name of list) {
      if (byName.has(name)) {
        tagIDs.push(byName.get(name))
      } else {
        const created = await http.post('/tags', { name })
        tagIDs.push(created.data?.id)
      }
    }
    await run(add ? 'add_tags' : 'remove_tags', { tag_ids: tagIDs.filter(Boolean) }, add ? '已加标签' : '已移除标签')
  }

  async function onMeta() {
    const vals = await dialogs.form({
      title: '修改元数据',
      fields: [
        {
          key: 'taken',
          label: '拍摄时间（YYYY-MM-DD，留空 = 不修改；输 - 清除）',
          validate: (v) => {
            const s = v.trim()
            if (!s || s === '-') return ''
            return Number.isNaN(new Date(s).getTime()) ? '日期格式不合法' : ''
          }
        },
        { key: 'place', label: '地点（留空 = 不修改；输 - 清除）' }
      ]
    })
    if (!vals) return
    const params = {}
    const taken = vals.taken.trim()
    const place = vals.place.trim()
    if (taken) {
      params.taken_at = taken === '-' ? null : new Date(taken).toISOString()
    }
    if (place) {
      params.place = place === '-' ? '' : place
    }
    if (!Object.keys(params).length) return
    await run('set_meta', params, '元数据已更新')
  }

  async function onShare() {
    // 分享=逐条创建外链（分享语义按单个媒体）；批量场景通常选少量
    if (busy.value) return
    busy.value = true
    let ok = 0
    try {
      for (const id of ids()) {
        try {
          await http.post('/shares', { kind: 'media', target_id: id })
          ok++
        } catch {
          /* 单条失败不阻断 */
        }
      }
      lastResult.value = `已创建 ${ok}/${selected.size} 个分享链接（管理后台可见）`
      // 统一约定：成功即清空选择集并通知宿主刷新（与 run() 对齐，Job000099 修正此前漏发）
      selected.clear()
      onChanged?.()
    } finally {
      busy.value = false
    }
  }

  return {
    selected,
    count,
    busy,
    lastResult,
    toggle,
    clear,
    isAllSelected,
    toggleAll,
    invertAll,
    onDelete,
    onMove,
    onCopy,
    onShare,
    onShareSpace,
    onTags,
    onMeta
  }
}

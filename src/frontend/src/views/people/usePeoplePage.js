import { onMounted, reactive, ref } from 'vue'
import http from '../../api/http'
import { loadThumbUrl } from '../../components/timeline/mediaLoader'
import { dialogs } from '../../components/dialogs/dialogs'

// PeopleView 拆解（Job000090）：人物页数据与操作编排——
// 数据加载（named/unnamed/covers 封面 objectURL 池）/双勾选集（未命名聚类合并用 + 已命名批量隐藏用）/
// 批量隐藏（confirm + PATCH 循环逐人独立成败）/合并命名与改名（统一对话框宿主编排）/
// 单个隐藏/人脸重算扫描。openPerson 页面跳转属视图职责，留宿主。
//
// API 契约 v1.1 §6 人物：
//   GET   /people      → { named:[...], unnamed:[{cluster_id,count,cover}] }
//   POST   /people      → 命名并合并：{ cluster_ids:[...], name, is_pet }
//   PATCH  /people/:id  → { name } | { hidden }
//   POST   /ai/faces    → 主动重算：{ scope:"all" }
// 说明：人脸缩略图复用媒体缩略图端点 GET /media/:id/thumb?size=sm（尚无「人脸裁剪图」端点）。
// Job000081 实体级批量：已命名多选 → 批量隐藏/取消隐藏；named 合并需后端支持（named 不返
// cluster_ids），本轮不做——登记簿已挂未来项。
export function usePeoplePage() {
  const named = ref([])
  const unnamed = ref([])
  const covers = reactive({}) // key(personId|clusterId) -> objectURL
  const loading = ref(false)
  const loadError = ref('')
  const selected = ref([]) // 未命名聚类勾选（合并用）
  const namedSelected = ref([]) // Job000081 已命名人物勾选（批量隐藏用）

  const scanning = ref(false)
  const scanNotice = ref('')

  function errMsg(e, fallback) {
    return e?.response?.data?.error?.message || fallback
  }

  // 封面媒体 ID：命名人物用 cover_media_id，未命名聚类用 cover
  function coverId(entry) {
    return entry?.cover_media_id || entry?.cover || entry?.first_media_id || null
  }

  async function loadCover(key, mediaId) {
    if (!mediaId || covers[key]) return
    try {
      covers[key] = await loadThumbUrl({ id: mediaId }, 'sm')
    } catch (e) {
      // 缩略图缺失不致命：卡片显示占位符
    }
  }

  async function load() {
    loading.value = true
    loadError.value = ''
    try {
      const { data } = await http.get('/people')
      named.value = Array.isArray(data?.named) ? data.named : []
      unnamed.value = Array.isArray(data?.unnamed) ? data.unnamed : []
      selected.value = []
      namedSelected.value = []
      for (const p of named.value) loadCover(p.id, coverId(p))
      for (const c of unnamed.value) loadCover(c.cluster_id, coverId(c))
    } catch (e) {
      loadError.value = errMsg(e, '人物列表加载失败')
    } finally {
      loading.value = false
    }
  }

  function togglePick(clusterId) {
    const i = selected.value.indexOf(clusterId)
    if (i >= 0) selected.value.splice(i, 1)
    else selected.value.push(clusterId)
  }

  // ---- Job000081 已命名人物：多选 + 批量隐藏 ----
  function toggleNamedPick(id) {
    const i = namedSelected.value.indexOf(id)
    if (i >= 0) namedSelected.value.splice(i, 1)
    else namedSelected.value.push(id)
  }

  // Job000099 全选/反选统一（人物实体集，语义同媒体批量：成功清空、失败保留）
  const allNamedPicked = () =>
    named.value.length > 0 && named.value.every((p) => namedSelected.value.includes(p.id))

  function toggleAllNamed() {
    namedSelected.value = allNamedPicked() ? [] : named.value.map((p) => p.id)
  }

  function invertNamed() {
    namedSelected.value = named.value
      .filter((p) => !namedSelected.value.includes(p.id))
      .map((p) => p.id)
  }

  const allClustersPicked = () =>
    unnamed.value.length > 0 && unnamed.value.every((c) => selected.value.includes(c.cluster_id))

  function toggleAllClusters() {
    selected.value = allClustersPicked() ? [] : unnamed.value.map((c) => c.cluster_id)
  }

  function invertClusters() {
    selected.value = unnamed.value
      .filter((c) => !selected.value.includes(c.cluster_id))
      .map((c) => c.cluster_id)
  }

  async function batchHide(hidden) {
    const targets = named.value.filter((p) => namedSelected.value.includes(p.id))
    if (!targets.length) return
    const ok = await dialogs.confirm({
      title: hidden ? '隐藏人物' : '取消隐藏人物',
      text: `将${hidden ? '隐藏' : '取消隐藏'}选中的 ${targets.length} 位人物。`,
      confirmText: hidden ? '隐藏' : '取消隐藏'
    })
    if (!ok) return
    let fail = 0
    for (const p of targets) {
      try {
        await http.patch(`/people/${p.id}`, { hidden })
        p.hidden = hidden
      } catch (e) {
        fail++
      }
    }
    namedSelected.value = []
    if (fail) scanNotice.value = `批量操作完成，${fail} 项失败`
  }

  async function openMerge() {
    const vals = await dialogs.form({
      title: '命名并合并',
      fields: [
        {
          key: 'name',
          label: `将选中的 ${selected.value.length} 个聚类合并为同一个人物，姓名`,
          placeholder: '如：张三',
          validate: (v) => (v && v.trim() ? '' : '姓名不能为空')
        },
        { key: 'pet', label: '这是宠物', type: 'check', initial: false }
      ]
    })
    if (!vals) return
    try {
      await http.post('/people', {
        cluster_ids: [...selected.value],
        name: vals.name.trim(),
        is_pet: !!vals.pet
      })
      await load()
    } catch (e) {
      await dialogs.alert(errMsg(e, '合并失败'))
    }
  }

  async function startRename(p) {
    const name = await dialogs.prompt({
      title: '修改姓名',
      label: '姓名',
      initial: p.name || '',
      validate: (v) => (v && v.trim() ? '' : '姓名不能为空')
    })
    if (name === null) return
    try {
      await http.patch(`/people/${p.id}`, { name: name.trim() })
      await load()
    } catch (e) {
      await dialogs.alert(errMsg(e, '改名失败'))
    }
  }

  async function toggleHidden(p) {
    try {
      await http.patch(`/people/${p.id}`, { hidden: !p.hidden })
      p.hidden = !p.hidden
    } catch (e) {
      scanNotice.value = errMsg(e, '操作失败')
    }
  }

  async function triggerScan() {
    if (scanning.value) return
    scanning.value = true
    scanNotice.value = ''
    try {
      const { data } = await http.post('/ai/faces', { scope: 'all' })
      scanNotice.value = data?.job_id
        ? `已提交人脸重算任务（job ${data.job_id}），完成后刷新本页查看结果`
        : '已提交人脸重算任务，完成后刷新本页查看结果'
    } catch (e) {
      scanNotice.value = errMsg(e, '提交人脸重算失败')
    } finally {
      scanning.value = false
    }
  }

  onMounted(load)

  return {
    named, unnamed, covers, loading, loadError,
    selected, namedSelected, scanning, scanNotice,
    load, togglePick, toggleNamedPick, batchHide,
    allNamedPicked, toggleAllNamed, invertNamed,
    allClustersPicked, toggleAllClusters, invertClusters,
    openMerge, startRename, toggleHidden, triggerScan
  }
}

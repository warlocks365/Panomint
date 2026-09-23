// 文件夹页面数据编排（Job000092：FoldersView 拆页面级 composable，与 090/091 同范式）。
// 职责：目录树/全量媒体两级加载、选中路径与路由 query 双向同步、面包屑段推导、
// client 侧前缀过滤、目录管理对话框编排（owner 节点开放改名/权限/删除）。
// 视图职责留宿主：openItem 跳转 player（router 属视图）、页级布局与网格容器。
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import http from '../../api/http'

export function useFoldersPage() {
  const router = useRouter()
  const route = useRoute()

  // ---- 目录管理（Job000069）：选中节点 owner 时开放 改名/权限/删除 ----
  const manageMode = ref('')
  const users = ref([])

  const tree = ref(null)
  const treeLoading = ref(true)
  // 刷新/分享链接可还原目录：?folder=<path>（Job000069 重构时 selectFolder 被误删、
  // 选中态永远停在空串，该轮补回并加上路由同步）
  const selectedPath = ref(typeof route.query.folder === 'string' ? route.query.folder : '')
  const loadError = ref('')

  const mediaItems = ref([])
  const mediaLoading = ref(true)

  const selectedNode = computed(() => findNode(tree.value, selectedPath.value))
  const selectedOwner = computed(() => !!selectedNode.value?.owner)
  const selectedGrants = computed(() => selectedNode.value?.grants || [])

  const totalCount = computed(() => tree.value?.count ?? mediaItems.value.length)

  // 面包屑：把选中路径拆成逐级可点的祖先段（全部 / a / a/b …）
  const pathSegments = computed(() => {
    if (!selectedPath.value) return []
    const segs = []
    let acc = ''
    for (const part of selectedPath.value.split('/').filter(Boolean)) {
      acc = acc ? acc + '/' + part : part
      segs.push({ name: part, path: acc })
    }
    return segs
  })

  // 当前目录媒体：folder_path 等于选中路径，或位于其子目录下
  const filteredItems = computed(() => {
    if (!selectedPath.value) return mediaItems.value
    const prefix = selectedPath.value + '/'
    return mediaItems.value.filter((m) => {
      const fp = m.folder_path || ''
      return fp === selectedPath.value || fp.startsWith(prefix)
    })
  })

  function findNode(node, path) {
    if (!node) return null
    if (node.path === path) return node
    for (const ch of node.children || []) {
      const hit = findNode(ch, path)
      if (hit) return hit
    }
    return null
  }

  function openManage(mode) {
    manageMode.value = mode
  }

  function closeManage() {
    manageMode.value = ''
  }

  async function onManaged() {
    manageMode.value = ''
    await Promise.all([loadTree(), reloadMedia()])
  }

  // 目录切换：全量媒体已在内存，client 侧过滤即时生效（任意层级切换立即重载）；
  // 同时写回路由 query 保持路径同步。
  // 挂载点在 selectFolder 之前的旧实现只有 selectedPath.value = path 一行，
  // Job000069 补管理功能时被整块误删——本函数即该轮补回版。
  function selectFolder(path) {
    selectedPath.value = path
    const q = { ...route.query }
    if (path) q.folder = path
    else delete q.folder
    router.replace({ query: q })
  }

  async function fetchAllMedia() {
    const items = []
    let cursor = ''
    // 拉取全量（游标分页，上限保护 20 页）
    for (let i = 0; i < 20; i++) {
      const params = { limit: 200 }
      if (cursor) params.cursor = cursor
      const res = await http.get('/media', { params, timeout: 0 }) // 全量连拉上限 20 页，慢网/大库下 15s 默认超时不够
      items.push(...(res.data.items || []))
      cursor = res.data.next_cursor || ''
      if (!cursor) break
    }
    return items
  }

  function reloadMedia() {
    mediaItems.value = []
    mediaLoading.value = true
    fetchAllMedia().then((items) => {
      mediaItems.value = items
    }).catch((e) => {
      loadError.value = '媒体列表加载失败：' + (e.response?.data?.error?.message || '网络错误')
    }).finally(() => {
      mediaLoading.value = false
    })
  }

  async function loadTree() {
    const res = await http.get('/folders/tree')
    tree.value = res.data
  }

  onMounted(async () => {
    try {
      await loadTree()
      // 路由带入的目录若既未注册也无媒体（树里不存在），回退到全部，
      // 避免标题/面包屑显示一个不可达的空白路径
      if (selectedPath.value && !findNode(tree.value, selectedPath.value)) {
        selectFolder('')
      }
    } catch (e) {
      loadError.value = '目录树加载失败：' + (e.response?.data?.error?.message || '网络错误')
    } finally {
      treeLoading.value = false
    }

    // 授权对话框的用户候选（管理端点 owner/admin 可用；失败不阻塞主流程）。
    try {
      const res = await http.get('/admin/users')
      users.value = res.data.users || []
    } catch {
      users.value = []
    }

    try {
      mediaItems.value = await fetchAllMedia()
    } catch (e) {
      loadError.value = '媒体列表加载失败：' + (e.response?.data?.error?.message || '网络错误')
    } finally {
      mediaLoading.value = false
    }
  })

  return {
    tree, treeLoading, selectedPath, loadError,
    mediaItems, mediaLoading, totalCount,
    pathSegments, filteredItems,
    selectFolder, reloadMedia,
    manageMode, users, selectedOwner, selectedGrants,
    openManage, closeManage, onManaged
  }
}

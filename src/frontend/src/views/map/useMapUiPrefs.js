// 地图界面偏好（Job000096 拆自 MapView，语义原样迁移）：账户级持久化 + 媒体类型过滤 + 移动端筛选浮层开关。
// 先以默认值渲染，拉到服务端偏好后再覆盖；偏好接口失败绝不拦住地图渲染（整体吞异常退回默认）。
// patchPrefs 先落本地（界面立刻响应）再 500ms 防抖写服务端；保存失败保留本地值并上报警告（不清 prefDirty，
// 留给卸载时补写重试）。flushOnUnmount：500ms 防抖窗口内离开页面会静默丢掉最后一次选择，故卸载时补写一次（不 await）。
import { computed, ref } from 'vue'
import { getUiPrefs, putUiPrefs } from '../../api/map'

export function useMapUiPrefs({ errRef }) {
  const uiPrefs = ref({
    map_slider_pos: 'bottom',
    map_filter_side: 'left',
    map_filter_collapsed: false, // Job000059：桌面筛选悬浮化收起状态
    map_marker_mode: 'icon', // Job000060：标记样式 icon|thumb
    map_default_provider: 'auto',
    map_default_zoom: null
  })
  const kind = ref('all') // 媒体类型过滤 all|photo|video|pano
  const filterOpen = ref(false) // 移动端筛选浮层开关

  // Job000060：标记样式 icon|thumb（记忆 uiPrefs.map_marker_mode，默认图标）
  const markerMode = computed(() => (uiPrefs.value.map_marker_mode === 'thumb' ? 'thumb' : 'icon'))

  async function loadUiPrefs() {
    try {
      const d = (await getUiPrefs()).data || {}
      uiPrefs.value = {
        map_slider_pos: d.map_slider_pos || 'bottom',
        map_filter_side: d.map_filter_side || 'left',
        map_filter_collapsed: d.map_filter_collapsed === true,
        map_marker_mode: d.map_marker_mode === 'thumb' ? 'thumb' : 'icon',
        map_default_provider: d.map_default_provider || 'auto',
        map_default_zoom: typeof d.map_default_zoom === 'number' ? d.map_default_zoom : null
      }
    } catch {
      // 保持默认值即可，不提示（用户没做任何操作，弹错误只会困惑）
    }
  }

  let prefsSaveTimer = null // 界面偏好防抖写入（手写定时器，项目内无防抖工具）
  let prefDirty = false // 有尚未落到服务端的偏好改动

  // 先落本地（界面立刻响应），再防抖写服务端
  function patchPrefs(partial) {
    uiPrefs.value = { ...uiPrefs.value, ...partial }
    prefDirty = true
    clearTimeout(prefsSaveTimer)
    prefsSaveTimer = setTimeout(saveUiPrefs, 500)
  }

  async function saveUiPrefs() {
    try {
      await putUiPrefs({ ...uiPrefs.value })
      prefDirty = false
    } catch {
      // 保存失败保留本地值：界面与用户的选择保持一致，仅非阻塞提示
      // 不清 prefDirty：留给卸载时的补写重试
      errRef.value = '界面偏好已本地生效，服务端同步失败'
    }
  }

  function setFilterCollapsed(v) {
    patchPrefs({ map_filter_collapsed: !!v })
  }

  function setMarkerMode(m) {
    patchPrefs({ map_marker_mode: m === 'thumb' ? 'thumb' : 'icon' })
  }

  function flushOnUnmount() {
    clearTimeout(prefsSaveTimer)
    if (prefDirty) saveUiPrefs()
  }

  function destroy() {
    clearTimeout(prefsSaveTimer)
  }

  return {
    uiPrefs,
    kind,
    filterOpen,
    markerMode,
    loadUiPrefs,
    patchPrefs,
    setFilterCollapsed,
    setMarkerMode,
    flushOnUnmount,
    destroy
  }
}

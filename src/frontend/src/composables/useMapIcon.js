// 地图标记图标偏好——MapView 的 composable 抽取（Job000060，行数棘轮倒逼，语义原样迁移）。
// 含：形状 path 表、SVG dataURL 生成、注册到 MapLibre、账户级偏好加载/保存（服务端失败降级 localStorage）。
// makeDefaultIconImageData 同步生成默认红点 ImageData——图层挂载前即可注册 cluster-icon，
// 保证缩略图层的 coalesce 回退在图标异步加载完成前也有形可见（修"切换不及时/标记消失"）。
import { ref } from 'vue'
import { getMapIconPref, putMapIconPref } from '../api/map'
import { MAP_ICON_SHAPE_PATHS, isMapIconShape } from './mapIconShapes'

export function makeDefaultIconImageData() {
  const size = 24
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const ctx = canvas.getContext('2d')
  ctx.beginPath()
  ctx.arc(size / 2, size / 2, 8, 0, Math.PI * 2)
  ctx.fillStyle = '#ef4444' // PIN_COLOR_PALETTE 同族豁免：与 DEFAULT_PIN_COLOR 同步默认红点
  ctx.fill()
  ctx.lineWidth = 2.5
  ctx.strokeStyle = '#ffffff'
  ctx.stroke()
  return ctx.getImageData(0, 0, size, size)
}

export function useMapIcon(getMap) {
  // PIN_COLOR_PALETTE 同族豁免：图钉调色板默认色（颜色即数据，非样式债）
  const DEFAULT_PIN_COLOR = '#ef4444'
  const iconPref = ref({ shape: 'circle', color: DEFAULT_PIN_COLOR })
  // applyIcon 加载成功的图标元素——缩略图模式加载失败时回退复用（MapView.ensureThumbImage 消费）
  const clusterIconEl = ref(null)

  // 形状 path 表已上移到 mapIconShapes.js（与选择器共用单一真源）。
  // 此前本文件另有一份 SHAPE_PATHS 且只含 circle/pin，与选择器提供的四种形状
  // 不一致，导致点三角形/菱形/五角星时静默失效（详见 mapIconShapes.js 顶部说明）。
  // 注：旧表里的 pin 项其实是死代码——shape==='pin' 在下方已被 PNG 分支提前拦掉。

  function shapeSvgDataUrl(shape, color) {
    // 未知形状不再静默返回 null：回退到圆形并留下痕迹。
    // 静默 return 会让「界面已把新形状标为选中、地图却仍是旧图标」——
    // 用户无从判断是点了没生效还是自己看错了（这正是本次缺陷最难自查的原因）。
    const key = isMapIconShape(shape) ? shape : 'circle'
    if (!isMapIconShape(shape)) {
      console.warn('[mapIcon] 未知基础形状，回退为圆形：', shape)
    }
    const path = MAP_ICON_SHAPE_PATHS[key]
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="${color}">${path}</g></svg>`
    return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg)
  }

  // 加载图标到 map（矢量 SVG → dataURL；内置 PNG → 静态路径；自定义 → dataURL）
  function applyIcon(errRef) {
    const map = getMap()
    if (!map || !map.hasImage) return
    const pref = iconPref.value
    let url = null
    if (pref.shape === 'pin') url = '/map-icons/pin.png'
    else if (pref.shape === 'inverted') url = '/map-icons/inverted.png'
    else if (pref.shape === 'custom' && pref.data_url) url = pref.data_url
    else url = shapeSvgDataUrl(pref.shape, pref.color || DEFAULT_PIN_COLOR)

    if (!url) return
    const img = new Image()
    img.onload = () => {
      const m = getMap()
      if (!m) return
      // 尺寸统一 24px；删除旧图标避免累积。保留元素引用供缩略图加载失败回退复用
      clusterIconEl.value = img
      if (m.hasImage('cluster-icon')) m.removeImage('cluster-icon')
      m.addImage('cluster-icon', img, { sdf: false })
    }
    img.src = url
  }

  async function loadIconPref() {
    try {
      const p = await getMapIconPref()
      if (p && p.shape) {
        iconPref.value = { shape: p.shape, color: p.color || DEFAULT_PIN_COLOR, data_url: p.data_url || '' }
      }
    } catch {
      // 服务端不可达 → 读本地兜底
      try {
        const local = localStorage.getItem('map_icon_pref')
        if (local) iconPref.value = JSON.parse(local)
      } catch {}
    }
  }

  async function onIconPrefUpdate(pref) {
    iconPref.value = pref
    applyIcon()
    // 本地兜底 + 服务端持久化
    try { localStorage.setItem('map_icon_pref', JSON.stringify(pref)) } catch {}
    try {
      await putMapIconPref(pref)
    } catch {
      if (errRef) errRef.value = '图标偏好已本地保存，服务端同步失败'
    }
  }

  return { iconPref, clusterIconEl, applyIcon, loadIconPref, onIconPrefUpdate }
}

// MapLibre 引擎生命周期（Job000096 拆自 MapView，一域自持）：建图/图源图层/事件接线/触摸长按/验收钩子/销毁。
// 宿主只持有容器 div（mapRef）并调 init/destroy；簇点击、悬停、长按、视野变化经注入 hooks 外发给
// list/hover/data 状态机（engine 不持有业务状态）。
// raster 底图经本站反代（Key 不下发浏览器），瓦片请求带 Bearer；图层挂载前同步注册默认红点，
// 保证缩略图层的 coalesce 回退在图标异步加载完成前也有形可见（修「切换不及时/标记消失」）。
import { Map as MapLibreMap, NavigationControl, setWorkerUrl } from 'maplibre-gl' // v6 纯 ESM
import 'maplibre-gl/dist/maplibre-gl.css'
import { getAccessToken } from '../../utils/tokenStore'
import { makeDefaultIconImageData } from '../../composables/useMapIcon'

export function useMapEngine(hooks) {
  // hooks: {
  //   getDefaultZoom()                 建图初始缩放（来自界面偏好，null → 4）
  //   getMarkerMode()                  图层初始显隐 icon|thumb
  //   onLoad()                         图层就位后（applyIcon）
  //   onReady()                        首帧数据加载（reload）
  //   onClusterClick(props, lngLat)    簇点击下钻/展开
  //   onHoverEnter(f, x, y)            mouseenter（120ms 延迟在 hover 状态机内）
  //   onHoverLeave()                   mouseleave（清延迟 + 排程关闭）
  //   onTouchShow(f, x, y)             长按 500ms 命中特征
  //   onTouchClear()                   长按移动超阈值取消
  //   onTouchClose()                   松手延迟关闭
  //   onMoveEnd()                      moveend/zoomend 防抖重载
  //   probe()                          { has, vis, openList } → window.__mapProbe（生产安全验收缝）
  // }
  let map = null
  let touchTimer = null // 长按 500ms 触发计时（独立于 hover 的 mouseenter 计时）
  let touchStartInfo = null

  // 高德栅格瓦片（经本站反代，Key 不下发浏览器）
  const rasterStyle = {
    version: 8,
    sources: {
      amap: {
        type: 'raster',
        tiles: ['/tiles/amap/{z}/{x}/{y}'],
        tileSize: 256,
        attribution: '© 高德地图'
      }
    },
    layers: [{ id: 'amap-base', type: 'raster', source: 'amap' }]
  }

  function getMap() {
    return map
  }

  function init(container) {
    setWorkerUrl('/maplibre-gl-worker.mjs')
    map = new MapLibreMap({
      container,
      style: rasterStyle,
      center: [116.397, 39.909],
      zoom: hooks.getDefaultZoom() ?? 4,
      transformRequest: (url) => {
        if (url.includes('/tiles/')) {
          const token = getAccessToken()
          if (token) return { url, headers: { Authorization: `Bearer ${token}` } }
        }
        return { url }
      }
    })
    map.addControl(new NavigationControl({ showCompass: false }), 'top-right')

    map.on('load', onMapLoad)
    wireTouch()
    map.on('moveend', hooks.onMoveEnd)
    map.on('zoomend', hooks.onMoveEnd)
  }

  function onMapLoad() {
    // 同步默认红点：图层挂载前注册，cluster-thumbs 的 coalesce 回退即刻有形（不依赖异步图标）
    map.addImage('cluster-icon', makeDefaultIconImageData())
    map.addSource('clusters', { type: 'geojson', data: { type: 'FeatureCollection', features: [] } })
    map.addLayer({
      id: 'cluster-circles',
      type: 'symbol',
      source: 'clusters',
      layout: {
        'icon-image': 'cluster-icon',
        'icon-size': 1,
        'icon-allow-overlap': true,
        visibility: hooks.getMarkerMode() === 'icon' ? 'visible' : 'none'
      }
    })
    // Job000060 缩略图模式：同一份簇数据按 icon_img 属性渲染代表图；
    // 加载失败的簇在 ensureThumbImage 里被注册为回退图标，属性无需变更
    map.addLayer({
      id: 'cluster-thumbs',
      type: 'symbol',
      source: 'clusters',
      layout: {
        'icon-image': ['coalesce', ['get', 'icon_img'], 'cluster-icon'],
        'icon-size': 0.3,
        'icon-anchor': 'center',
        'icon-allow-overlap': true,
        visibility: hooks.getMarkerMode() === 'thumb' ? 'visible' : 'none'
      }
    })

    // 加载初始图标（默认红点/偏好图标）
    hooks.onLoad()

    // 点击下钻 / 展开 + 悬停预览（两种标记图层同逻辑）
    for (const layerId of ['cluster-circles', 'cluster-thumbs']) {
      map.on('click', layerId, (e) => {
        const f = e.features?.[0]
        if (!f) return
        hooks.onClusterClick(f.properties, f.geometry.coordinates.slice())
      })
      map.on('mouseenter', layerId, (e) => {
        const f = e.features?.[0]
        if (!f) return
        hooks.onHoverEnter(f, e.originalEvent.clientX, e.originalEvent.clientY)
      })
      map.on('mousemove', layerId, () => {
        map.getCanvas().style.cursor = 'pointer'
      })
      map.on('mouseleave', layerId, () => {
        map.getCanvas().style.cursor = ''
        // 不立即关闭：给用户移入卡片点选的时间（清延迟 + 250ms 排程在 hover 状态机内）
        hooks.onHoverLeave()
      })
    }

    if (import.meta.env.DEV) window.__map = map
    // 验收探针钩子（生产可用，无副作用）：验证缩略图注册与图层可见性；openList 走真实 openCluster 生产路径打开「此处 N 项」面板
    window.__mapProbe = hooks.probe()
    hooks.onReady()
  }

  // 长按预览（触摸）：500ms 未移动触发，移动超阈值取消（与平移互斥）
  function wireTouch() {
    const canvas = map.getCanvas()
    canvas.addEventListener('touchstart', (e) => {
      if (e.touches.length !== 1) return
      const t = e.touches[0]
      touchStartInfo = { x: t.clientX, y: t.clientY, t: Date.now() }
      clearTimeout(touchTimer)
      touchTimer = setTimeout(() => {
        const fs = map.queryRenderedFeatures([t.clientX, t.clientY], { layers: ['cluster-circles', 'cluster-thumbs'] })
        if (fs.length) hooks.onTouchShow(fs[0], t.clientX, t.clientY)
        touchStartInfo = null
      }, 500)
    }, { passive: true })
    canvas.addEventListener('touchmove', (e) => {
      if (!touchStartInfo || !e.touches.length) return
      const t = e.touches[0]
      const dx = t.clientX - touchStartInfo.x
      const dy = t.clientY - touchStartInfo.y
      if (dx * dx + dy * dy > 100) { // 移动超过 10px → 判定为平移，取消长按
        clearTimeout(touchTimer)
        hooks.onTouchClear()
        touchStartInfo = null
      }
    }, { passive: true })
    canvas.addEventListener('touchend', () => {
      clearTimeout(touchTimer)
      // 延迟关闭预览（长按松手后短暂停留，给点选时间）
      hooks.onTouchClose()
      touchStartInfo = null
    }, { passive: true })
  }

  function destroy() {
    clearTimeout(touchTimer)
    map?.remove()
    map = null
    if (import.meta.env.DEV) window.__map = null
  }

  return { getMap, init, destroy }
}

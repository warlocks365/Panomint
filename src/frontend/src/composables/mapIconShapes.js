/**
 * 地图标记「基础形状」的唯一真源。
 *
 * 注意（Job000145 缺陷修复的根因）：
 * 此前形状定义散在两处，且**坐标系不同**——
 *   · MapIconPicker.vue 的预览：viewBox="0 0 18 18"，path 在 18 单位盒里
 *   · useMapIcon.js 的 SHAPE_PATHS：viewBox="0 0 24 24"，path 在 24 单位盒里
 * 两处独立维护，必然漂移。实际后果是：选择器提供 circle/triangle/diamond/star
 * 四种，而 SHAPE_PATHS 只有 circle 与 pin，于是点后三种时
 * `SHAPE_PATHS[shape]` 为 undefined → 生成函数返回 null → applyIcon 静默 return
 * → 地图保持旧图标，用户看到的是「点了没反应」，且**界面已把新形状标为选中**。
 *
 * 现在两处都从这里取，坐标系统一为 **24×24**，预览侧把 viewBox 也改成 24
 * （SVG 自带缩放，显示尺寸仍是 18px），从而**预览与地图渲染逐像素同源**。
 *
 * 尺寸基准：现有圆形 r=9 / 24 盒 = 75% 占比，其余形状按同一占比设计，
 * 避免四种形状大小不一。
 */
export const MAP_ICON_SHAPES = [
  { key: 'circle', name: '圆形', path: '<circle cx="12" cy="12" r="9"/>' },
  { key: 'triangle', name: '三角形', path: '<path d="M12 3 L21 21 H3 Z"/>' },
  { key: 'diamond', name: '菱形', path: '<path d="M12 3 L21 12 L12 21 L3 12 Z"/>' },
  // 五角星：外接半径 9.5 ≈ 圆形的直径 18，与其余形状视觉等大
  {
    key: 'star',
    name: '五角星',
    path: '<path d="M12 2.5 L14.4 8.8 L21 9.1 L15.8 13.2 L17.6 19.7 L12 16 L6.4 19.7 L8.2 13.2 L3 9.1 L9.6 8.8 Z"/>'
  }
]

/** key → path 的查表（供地图侧渲染用） */
export const MAP_ICON_SHAPE_PATHS = MAP_ICON_SHAPES.reduce((m, s) => {
  m[s.key] = s.path
  return m
}, {})

/** 该 key 是否为受支持的基础形状——选择器与地图侧共用的判定，避免两处各判一次 */
export function isMapIconShape(key) {
  return Object.prototype.hasOwnProperty.call(MAP_ICON_SHAPE_PATHS, key)
}
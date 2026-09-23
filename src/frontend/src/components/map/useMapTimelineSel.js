import { computed, ref } from 'vue'

// MapTimeline 拆解（Job000089）：选择/拖拽/统计核心逻辑——
// bars 过滤排序映射（剔除 unknown 桶）+ 拖拽框选状态机（位移阈值区分单击/范围）
// + 已提交 range 反推高亮索引 + 四类统计 + bucket↔ISO 区间换算。
// trackRef 由宿主传入并模板绑定（DOM 归属宿主；静态 ref="a.b" 字符串不走对象路径，会静默成 null）。
export function useMapTimelineSel(props, emit, trackRef) {
  /* ---------- bars：bucket key 正则随粒度变化（year=YYYY / month=YYYY-MM / day=YYYY-MM-DD） ---------- */
  const keyRe = computed(() => {
    if (props.granularity === 'year') return /^\d{4}$/
    if (props.granularity === 'day') return /^\d{4}-\d{2}-\d{2}$/
    return /^\d{4}-\d{2}$/
  })

  // 'unknown' 桶（taken_at 为空）无法定位时间轴，剔除
  const bars = computed(() =>
    props.buckets
      .filter((b) => b && keyRe.value.test(b.bucket))
      .sort((a, b) => (a.bucket < b.bucket ? -1 : 1))
      .map((b) => ({
        key: b.bucket,
        count: b.count || 0,
        photos: b.photos || 0,
        videos: b.videos || 0,
        panoPhotos: b.pano_photos || 0,
        panoVideos: b.pano_videos || 0
      }))
  )

  const maxCount = computed(() => Math.max(1, ...bars.value.map((b) => b.count)))

  function barHeight(count) {
    return Math.max(6, Math.round((count / maxCount.value) * 100))
  }

  function barTitle(b) {
    return `${b.key} · 照片${b.photos} 视频${b.videos} 全景照片${b.panoPhotos} 全景视频${b.panoVideos}`
  }

  /* ---------- 拖拽框选状态机 ---------- */
  const dragging = ref(false)
  const sel = ref([-1, -1]) // 框选中的索引区间（未提交）
  const downX = ref(0) // 按下时的 clientX（位移阈值，区分单击/拖拽）
  const downIndex = ref(-1) // 按下时的 bucket 索引

  // 由已提交的 range（ISO）反推高亮索引（按当前粒度截断 bucket 前缀）
  const committedIdx = computed(() => {
    if (!props.range) return [-1, -1]
    const keys = bars.value.map((b) => b.key)
    const prefixLen = { year: 4, month: 7, day: 10 }[props.granularity] || 7
    const prefixOf = (iso) => (iso || '').slice(0, prefixLen)
    const a = keys.indexOf(prefixOf(props.range.from))
    const b = keys.indexOf(prefixOf(props.range.to))
    if (a < 0 || b < 0) return [-1, -1]
    return [Math.min(a, b), Math.max(a, b)]
  })

  const active = computed(() => (dragging.value ? sel.value : committedIdx.value))
  const hasRange = computed(() => active.value[0] >= 0)

  function inRange(i) {
    const [a, b] = active.value
    return a >= 0 && i >= a && i <= b
  }

  const rangeLabel = computed(() => {
    const [a, b] = active.value
    if (a < 0) return '全部时间'
    return a === b ? bars.value[a].key : `${bars.value[a].key} ~ ${bars.value[b].key}`
  })

  // 四类统计：默认统计全视野；有框选时统计选中区间
  const statTotals = computed(() => {
    let list = bars.value
    const [a, b] = active.value
    if (a >= 0 && b >= a) list = bars.value.slice(a, b + 1)
    return list.reduce(
      (acc, b) => {
        acc.photos += b.photos
        acc.videos += b.videos
        acc.panoPhotos += b.panoPhotos
        acc.panoVideos += b.panoVideos
        return acc
      },
      { photos: 0, videos: 0, panoPhotos: 0, panoVideos: 0 }
    )
  })

  function indexAt(clientX) {
    const track = trackRef.value
    if (!track) return 0
    const rect = track.getBoundingClientRect()
    const ratio = (clientX - rect.left) / Math.max(1, rect.width)
    const i = Math.floor(ratio * bars.value.length)
    return Math.min(bars.value.length - 1, Math.max(0, i))
  }

  // 位移阈值（px）：pointermove 超过此值才视为拖拽，更新结束索引；
  // 否则视为单击（可能因浮点抖动派发 1~2px 的 pointermove，不改变选区）
  const DRAG_THRESHOLD = 6

  function onDown(e) {
    if (!bars.value.length) return
    dragging.value = true
    downX.value = e.clientX
    downIndex.value = indexAt(e.clientX)
    sel.value = [downIndex.value, downIndex.value]
    trackRef.value?.setPointerCapture?.(e.pointerId)
  }

  function onMove(e) {
    if (!dragging.value) return
    if (Math.abs(e.clientX - downX.value) < DRAG_THRESHOLD) return // 未达拖拽阈值，保持单选
    const i = indexAt(e.clientX)
    sel.value = [downIndex.value, i]
  }

  function onUp() {
    if (!dragging.value) return
    dragging.value = false
    const [a0, b0] = sel.value
    const a = Math.min(a0, b0)
    const b = Math.max(a0, b0)
    if (!bars.value[a] || !bars.value[b]) return
    // 判断单击 vs 拖拽：sel 未扩展（起止仍相同）→ 单击单选该 bucket；否则范围选择
    if (a === b) {
      emit('change', toRange(bars.value[a].key, bars.value[a].key))
    } else {
      emit('change', toRange(bars.value[a].key, bars.value[b].key))
    }
  }

  function clear() {
    sel.value = [-1, -1]
    emit('change', null)
  }

  /* ---------- bucket → ISO 区间（闭区间：起始 00:00 ~ 末尾 23:59:59.999） ---------- */
  // 注意：一律用 UTC 构造，避免 toISOString() 因本地时区导致月初/月末跨月错位
  // （否则 committedIdx 反推时 prefixOf(from/to) 与 bucket key 对不上，单选会退化为范围）
  function toRange(fromKey, toKey) {
    const from = bucketStart(fromKey)
    const to = bucketEnd(toKey)
    return { from: from.toISOString(), to: to.toISOString() }
  }

  function bucketStart(key) {
    if (/^\d{4}$/.test(key)) return new Date(Date.UTC(+key, 0, 1, 0, 0, 0, 0))
    if (/^\d{4}-\d{2}$/.test(key)) {
      const [y, m] = key.split('-').map(Number)
      return new Date(Date.UTC(y, m - 1, 1, 0, 0, 0, 0))
    }
    const [y, m, d] = key.split('-').map(Number)
    return new Date(Date.UTC(y, m - 1, d, 0, 0, 0, 0))
  }

  function bucketEnd(key) {
    if (/^\d{4}$/.test(key)) return new Date(Date.UTC(+key + 1, 0, 1, 0, 0, 0, -1))
    if (/^\d{4}-\d{2}$/.test(key)) {
      const [y, m] = key.split('-').map(Number)
      return new Date(Date.UTC(y, m, 1, 0, 0, 0, -1)) // 次月 1 日 -1ms
    }
    const [y, m, d] = key.split('-').map(Number)
    return new Date(Date.UTC(y, m - 1, d, 23, 59, 59, 999))
  }

  return {
    trackRef, bars, hasRange, rangeLabel, statTotals,
    inRange, barHeight, barTitle,
    onDown, onMove, onUp, clear
  }
}

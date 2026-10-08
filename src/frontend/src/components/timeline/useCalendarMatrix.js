import { computed, ref } from 'vue'
import { buildMonthMatrix, headerKeyOf } from './timelineDimensions'

// 日历月矩阵的**纯状态层**（Job000145）。从 DatePickerPopover 拆出——连同四态样式，
// 该组件会超 300 行组织红线（frontend_org_guard O1）。
//
// 这里只管「当前看哪个月 / 每格处于四态中的哪一态 / 能不能点」，不碰 DOM，
// 故可与 timelineDimensions 一起被单测表驱动覆盖。
//
// 四态（与设计文档 §2.3 一致）：
//   selectable 有照片且非未来 → 实底数字 + 主色圆点
//   empty     过去但无照片   → --color-text-secondary 灰显 + aria-disabled
//   future    未来日期       → --color-text-disabled + disabled（WCAG 1.4.3 失效组件例外）
//   out       跨月补位格     → 键为空，天然不可定位
// 另叠加 today（底色区分）与 anchor（唯一实底态）两个正交修饰。

const WEEK = ['一', '二', '三', '四', '五', '六', '日']

export function useCalendarMatrix(buckets) {
  const now = new Date()
  const cursorYear = ref(now.getFullYear())
  const cursorMonth = ref(now.getMonth() + 1)
  const pickedKey = ref('')
  const todayKey = headerKeyOf('day', now)

  // 桶键 → 计数。用 Map 而非每次遍历数组：42 格 × 桶数在 day 档可达数千次查找
  const counts = computed(() => {
    const map = new Map()
    for (const b of buckets.value || []) {
      if (b && typeof b.bucket === 'string' && b.bucket !== 'unknown') map.set(b.bucket, b.count || 0)
    }
    return map
  })

  // 允许翻页的范围：最早有照片的月份 → 当前月。不允许翻到未来的空月份。
  const minMonth = computed(() => {
    let min = null
    for (const k of counts.value.keys()) {
      const y = Number(k.slice(0, 4))
      const m = Number(k.slice(5, 7))
      if (!Number.isFinite(y) || !Number.isFinite(m) || m < 1 || m > 12) continue
      if (min === null || y * 12 + m < min) min = y * 12 + m
    }
    return min
  })

  const cursorIndex = computed(() => cursorYear.value * 12 + cursorMonth.value)

  const canPrev = computed(() => minMonth.value !== null && cursorIndex.value > minMonth.value)

  const canNext = computed(() => cursorIndex.value < now.getFullYear() * 12 + now.getMonth() + 1)

  const cells = computed(() =>
    buildMonthMatrix(cursorYear.value, cursorMonth.value).map((c) => {
      const future = c.ts > now.getTime()
      const hasPhoto = c.key ? (counts.value.get(c.key) || 0) > 0 : false
      return {
        ...c,
        future,
        hasPhoto,
        selectable: !!c.key && !future && hasPhoto,
        out: !c.inMonth,
        today: c.key === todayKey
      }
    })
  )

  const hasAny = computed(() => cells.value.some((c) => c.hasPhoto))

  /** 今天有照片才把游标落到今天；无照片则保持原状（不静默跳到别的月份）。 */
  function jumpToday() {
    if (!(counts.value.get(todayKey) > 0)) return false
    cursorYear.value = now.getFullYear()
    cursorMonth.value = now.getMonth() + 1
    pickedKey.value = todayKey
    return true
  }

  function shiftMonth(delta) {
    const idx = cursorIndex.value + delta
    cursorYear.value = Math.floor(idx / 12)
    cursorMonth.value = (idx % 12) + 1
    pickedKey.value = ''
  }

  /** 只接受当前视图内且可选的键，防止外部传入未来/补位格造成无效跳转。 */
  function select(key) {
    const c = cells.value.find((x) => x.key === key)
    if (c && c.selectable) pickedKey.value = key
  }

  return {
    WEEK,
    now,
    todayKey,
    cursorYear,
    cursorMonth,
    pickedKey,
    counts,
    canPrev,
    canNext,
    cells,
    hasAny,
    shiftMonth,
    jumpToday,
    select,
    clearPicked: () => {
      pickedKey.value = ''
    }
  }
}
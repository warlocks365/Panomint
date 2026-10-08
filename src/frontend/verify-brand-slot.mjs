// 品牌位接入验证（Job000144）——经 Vite 的 SSR 管线编译真实 .vue 组件后渲染。
//
// 为什么不用裸 node：.vue 需要 @vitejs/plugin-vue 编译，裸 node 无法 import。
// 为什么不用浏览器 headless：本机 Chrome 在当前环境执行后无任何输出（连 --version 都为空），
// 无法产出截图或 DOM。SSR 走的是**真实编译 + 真实组件**，只断言渲染结果，不模拟浏览器。
//
// 用法（在 src/frontend 下）：node verify-brand-slot.mjs
import { createSSRApp, h } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createServer } from 'vite'
import { readFileSync, existsSync, readdirSync } from 'node:fs'

const results = []
const check = (name, cond, detail) => results.push({ name, ok: !!cond, detail })

const server = await createServer({
  configFile: false,
  server: { middlewareMode: true },
  appType: 'custom',
  plugins: [(await import('@vitejs/plugin-vue')).default()]
})

try {
  const { default: BrandLogo } = await server.ssrLoadModule('/src/components/brand/BrandLogo.vue')
  const render = (props) =>
    renderToString(createSSRApp({ render: () => h(BrandLogo, props) }))

  const full = await render({})
  check('默认 variant=full → logo-full-128',
    full.includes('/brand/logo-full-128.png'), full.match(/\/brand\/[a-z0-9.-]+/)?.[0])
  check('全标记版固有尺寸 128x106（与文件真实像素一致）',
    full.includes('width="128"') && full.includes('height="106"'), full.match(/width="\d+" height="\d+"/)?.[0])

  const disc = await render({ variant: 'disc', height: 28, alt: '全景相册' })
  check("variant='disc' → logo-disc-64",
    disc.includes('/brand/logo-disc-64.png'), disc.match(/\/brand\/[a-z0-9.-]+/)?.[0])
  check('盘面版固有尺寸 64x64（与文件真实像素一致）',
    disc.includes('width="64"') && disc.includes('height="64"'), disc.match(/width="\d+" height="\d+"/)?.[0])
  check('alt 透传（图标态承担可访问名）', disc.includes('alt="全景相册"'), '')
  // Vue SSR 把空 alt 序列化成裸属性 alt（等价 alt=""），不能断言成 alt=""
  check("alt='' → 装饰性空 alt（避免读屏重复播报）",
    /<img[^>]*\salt[ >]/.test(await render({ alt: '' })), '')

  check('height=72 → 72px', (await render({ height: 72 })).includes('height:72px'), '')
  check("height='2rem' → 原样透传", (await render({ height: '2rem' })).includes('height:2rem'), '')

  // 非法 variant：开发期 validator 发警告；且**任何情况下都不能抛异常崩页面**
  // （生产构建会剥离 validator，组件必须自己兜住）。
  let warned = false
  const origWarn = console.warn
  console.warn = (...a) => { if (/Invalid prop/.test(String(a[0]))) warned = true; origWarn(...a) }
  let crashed = false
  let fallback = ''
  try {
    fallback = await render({ variant: 'nope' })
  } catch {
    crashed = true
  } finally {
    console.warn = origWarn
  }
  check('非法 variant 触发 validator 警告（开发期可见）', warned, '')
  check('非法 variant 不抛异常（生产构建已剥离 validator，须组件兜底）', !crashed, '')
  check('非法 variant 退回全标记版而非空图', fallback.includes('/brand/logo-full-128.png'), '')

  // 深色页（player/share）回归护栏：字标墨色对 #14181d 仅 1.09:1，这些视图不得引用全标记版。
  for (const v of ['views/PlayerView.vue', 'views/SharePublicView.vue', 'views/player']) {
    const p = 'src/' + v + (v.endsWith('.vue') ? '' : '')
    if (!existsSync(p)) { check(`${v} 存在`, false, '路径不存在'); continue }
    const src = v.endsWith('.vue') ? readFileSync(p, 'utf8') : ''
    if (src) {
      check(`${v} 未引用全标记版 Logo（AC-23）`, !/logo-full/.test(src), '')
    }
  }

  // 页头 topbar 不放 Logo（Spec §8.2 锁定）
  const shell = readFileSync('src/layout/AppShell.vue', 'utf8')
  check('页头 topbar 未放 Logo（Spec §8.2 锁定）', !/logo-full|logo-disc/.test(shell), '')

  // ---- Job000144 返工回归护栏：组件声明尺寸 / manifest sizes / 文件真实像素 三者必须一致 ----
  // 原始缺陷正是三者脱节：文件名承诺 256 而文件实为 663x551，组件也照抄了错值。
  const pngSize = (rel) => {
    // 传入的是 '/brand/xxx.png' 或 'brand/xxx.png'，统一去掉前导斜杠再落到 public/
    const buf = readFileSync('public/' + rel.replace(/^\/+/, ''))
    return { w: buf.readUInt32BE(16), h: buf.readUInt32BE(20) }   // PNG IHDR 宽高
  }
  const componentSrc = readFileSync('src/components/brand/BrandLogo.vue', 'utf8')
  for (const m of componentSrc.matchAll(/(\/brand\/[a-z0-9-]+\.png)',\s*width:\s*(\d+),\s*height:\s*(\d+)/g)) {
    const [, rel, w, h] = m
    const real = pngSize(rel)          // rel 形如 /brand/logo-full-128.png
    check(`BrandLogo 声明 ${rel} = ${w}x${h} 与文件真实像素一致`,
      real.w === Number(w) && real.h === Number(h), `实际 ${real.w}x${real.h}`)
  }
  // manifest 的 sizes 字段必须等于文件真实像素
  const manifest = JSON.parse(readFileSync('public/manifest.webmanifest', 'utf8'))
  for (const ic of manifest.icons) {
    if (!ic.src.startsWith('/brand/')) continue
    const real = pngSize(ic.src)
    check(`manifest sizes ${ic.sizes} 与 ${ic.src} 真实像素一致`,
      ic.sizes === `${real.w}x${real.h}`, `实际 ${real.w}x${real.h}`)
  }
  // 文件名标称数字必须命中真实像素（挡住「文件名说谎」）
  for (const f of readdirSync('public/brand')) {
    const digits = f.match(/(\d+)\.png$/)?.[1]
    if (!digits) continue
    const real = pngSize('brand/' + f)
    check(`${f} 文件名标称 ${digits} 命中真实像素 ${real.w}x${real.h}`,
      real.w === Number(digits) || real.h === Number(digits), '')
  }

  // SideNav 三态：单实例按 mode 切资产，不写两套模板。
  const { default: SideNav } = await server.ssrLoadModule('/src/layout/SideNav.vue')
  const { createPinia } = await import('pinia')
  const renderNav = async (mode) => {
    const app = createSSRApp({ render: () => h(SideNav, { mode }) })
    app.use(createPinia())
    return renderToString(app)
  }
  const navExpanded = await renderNav('expanded')
  const navIcon = await renderNav('icon')
  const navHidden = await renderNav('hidden')
  check('侧栏展开态 → 全标记版 logo-full-128',
    navExpanded.includes('/brand/logo-full-128.png'), navExpanded.match(/\/brand\/[a-z0-9-]+\.png/)?.[0])
  check('侧栏展开态带 sidebar--expanded 类',
    navExpanded.includes('sidebar--expanded'), '')
  check('侧栏图标态 → 盘面版 logo-disc-64',
    navIcon.includes('/brand/logo-disc-64.png'), navIcon.match(/\/brand\/[a-z0-9-]+\.png/)?.[0])
  check('侧栏图标态带 sidebar--icon 类', navIcon.includes('sidebar--icon'), '')
  check('侧栏图标态 alt 承担可访问名（紧邻文字已隐藏）',
    navIcon.includes('alt="全景相册"'), '')
  check('侧栏收起态整块 brand 由 CSS 隐藏，不换资产',
    navHidden.includes('/brand/logo-full-128.png') && navHidden.includes('sidebar--hidden'), '')
  // 注意按 <img> 元素计数而非按 "brand-logo" 字符串计数：类名是
  // `brand-logo brand-logo--full`，字符串会出现两次但元素只有一个。
  const logoImgs = (html) => (html.match(/<img[^>]*class="[^"]*brand-logo[^"]*"/g) || []).length
  check('侧栏两态各只渲染 1 个 logo <img>（无重复模板）',
    logoImgs(navExpanded) === 1 && logoImgs(navIcon) === 1,
    `expanded=${logoImgs(navExpanded)} icon=${logoImgs(navIcon)}`)

  // 三个认证页：主视觉位用 full-lg（256x213，显示高 72px → 2.96x，2x 屏有余量）
  for (const v of ['views/LoginView.vue', 'views/RegisterView.vue', 'views/SetupView.vue']) {
    const src = readFileSync('src/' + v, 'utf8')
    check(`${v} 引用 BrandLogo 且传 variant="full-lg" + block`,
      /<BrandLogo\b/.test(src) && /variant="full-lg"/.test(src) && /\sblock\b/.test(src), '')
    check(`${v} 引用了 BrandLogo 的 import`, /import BrandLogo from/.test(src), '')
    // 间距收在组件里（block 形态），页面内不应再有各写一份的 logo 样式
    check(`${v} 未重复定义 Logo 样式（间距由组件 block 提供）`,
      !/\.(login|setup)-logo\s*\{/.test(src), '')
  }

  // 认证三页必须用 2x 有余量的 256 版；侧栏保持 128（28px 显示高已3.8x 余量）
  const lg = await render({ variant: 'full-lg', height: 72, block: true, alt: '全景相册' })
  check("variant='full-lg' → logo-full-256",
    lg.includes('/brand/logo-full-256.png'), lg.match(/\/brand\/[a-z0-9.-]+/)?.[0])
  check('full-lg 固有尺寸 256x213（与文件真实像素一致）',
    lg.includes('width="256"') && lg.includes('height="213"'), lg.match(/width="\d+" height="\d+"/)?.[0])
  check('侧栏紧凑位仍用 logo-full-128（未误改为 256）',
    navExpanded.includes('/brand/logo-full-128.png'), navExpanded.match(/\/brand\/[a-z0-9.-]+/)?.[0])

  // O1 棘轮护栏：本次不得把任何原本 <=300 行的文件顶破 300 行
  // （RegisterView 曾因页内 Logo 样式被顶到 301 行，此处锁死该回归）。
  const { execSync } = await import('node:child_process')
  const touched = ['views/RegisterView.vue', 'views/SetupView.vue', 'layout/SideNav.vue',
    'components/brand/BrandLogo.vue']
  for (const v of touched) {
    const now = readFileSync('src/' + v, 'utf8').split('\n').length
    let before = now
    try {
      before = Number(execSync(`git show HEAD:src/frontend/src/${v}`, { encoding: 'utf8' })
        .split('\n').length)
    } catch { /* 新增文件无基线 */ }
    check(`${v} 未顶破 300 行（HEAD ${before} → ${now}）`,
      before > 300 || now <= 300, `${before}→${now}`)
  }
} finally {
  await server.close()
}

let bad = 0
for (const r of results) {
  if (!r.ok) bad++
  console.log(`${r.ok ? 'PASS' : 'FAIL'}  ${r.name}${r.detail ? '  [' + r.detail + ']' : ''}`)
}
console.log(`\nRESULT: ${bad === 0 ? 'OK' : 'FAIL'}  ${results.length - bad}/${results.length} 通过`)
process.exit(bad === 0 ? 0 : 1)
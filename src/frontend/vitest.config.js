import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// Job000145 前端测试配置。
//
// 与 vite.config.js **刻意分开**：那份配置带两个有副作用的插件——
// copyMaplibreWorker（构建期往 public/ 拷worker）与 injectSwVersion（构建后改写 dist/sw.js）。
// 它们挂在 buildStart / closeBundle 上，vitest 复用 vite.config.js 时会一并触发，
// 在 dist 不存在时抛 writeFile ENOENT，表现为「测试文件随机少一个」，极易误判为自身用例失败。
//
// 故测试环境只挂 @vitejs/plugin-vue，不引入任何有构建副作用的插件。
export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'node',
    // 按约定收集 `__tests__` 下的用例，**不再按目录白名单**。
    // 原为 `['src/components/timeline/__tests__/**']`，当时只有时间轴有测试，
    // 但白名单的代价是：任何写在别处的测试会被**静默忽略**（不是失败，是不收集）——
    // 本次新增 `src/composables/__tests__/mapIconShapes.spec.js` 时就踩到了，
    // 表现为「No test files found」，很容易误判成测试写错而非配置漏收。
    // `**/__tests__/**/*.spec.js` 本身已足够收敛（不会卷入 node_modules/dist）。
    include: ['src/**/__tests__/**/*.spec.js'],
    // 组件用例需 DOM（焦点/键盘/aria），故在文件内用 `// @vitest-environment jsdom` 逐个声明
    reporters: 'default',
    // 串行 + 单进程：多个 jsdom 用例并发时会在同一个临时目录上互相抢占
    // （表现为 Windows 上偶发 EPERM + 用例文件数随机少 1 + 退出码 1）。
    // 这类非确定性失败比它掩盖的问题更糟——它会让 CI 变成「随机红」，
    // 久而久之没人再看门禁。故显式串行：3 个文件 ~1s，不值得为并行付这个代价。
    fileParallelism: false,
    pool: 'forks',
    poolOptions: {
      forks: { singleFork: true }
    }
  }
})
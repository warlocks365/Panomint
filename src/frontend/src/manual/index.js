// Job000119 应用内操作手册——章节汇总入口（内容静态打包，无需后端）
import { chapters as basics } from './content-basics'
import { chapters as storage } from './content-storage'
import { chapters as admin } from './content-admin'
import { chapters as agent } from './content-agent'

export const manualChapters = [...basics, ...storage, ...admin, ...agent]

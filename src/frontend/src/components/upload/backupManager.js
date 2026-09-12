import { addFiles, queue } from './uploadManager'

// 移动端「前台增量备份」。
//
// ⚠️ 可行性裁决（重要，勿误解）：iOS/Safari **没有** Background Sync / Periodic Background
//    Sync / Background Fetch；Web 也没有枚举设备相册的 API。因此本模块只能在**页面处于前台**
//    时工作（用户打开 PWA → 选择新照片 → 立即上传）。它**不是**"后台自动备份"。
//    真正的自动备份请走：桌面端 Agent（rclone/WebDAV/Syncthing）或原生 App。
//
// 增量策略（与后端 sha256 去重叠加，双保险）：
//   1. 客户端 SubtleCrypto 计算文件 sha256（需 HTTPS 安全上下文）；
//   2. 与 IndexedDB 中"已上传"清单比对，跳过同内容文件（同内容换名/重复选择不再上传）；
//   3. 新文件交给 uploadManager 复用既有分块续传链路 POST /media/upload；
//   4. 上传成功（status=done）后把 sha256 记入 IndexedDB。
//   注：服务端 ingest 仍会按 hash+owner_id 去重（返回已存在 id），故即使清单丢失也不会重复入库。

const DB_NAME = 'pano-backup'
const DB_VERSION = 1
const STORE = 'uploaded'

function supported() {
  return typeof indexedDB !== 'undefined' && typeof crypto !== 'undefined' && !!crypto.subtle
}

function openDB() {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: 'hash' })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

async function hasHash(hash) {
  try {
    const db = await openDB()
    return await new Promise((resolve) => {
      const tx = db.transaction(STORE, 'readonly')
      const r = tx.objectStore(STORE).get(hash)
      r.onsuccess = () => resolve(!!r.result)
      r.onerror = () => resolve(false)
    })
  } catch {
    return false
  }
}

async function putHash(hash, meta) {
  try {
    const db = await openDB()
    await new Promise((resolve) => {
      const tx = db.transaction(STORE, 'readwrite')
      tx.objectStore(STORE).put({ hash, at: Date.now(), ...meta })
      tx.oncomplete = () => resolve()
      tx.onerror = () => resolve()
    })
  } catch {
    /* 忽略：清单只是优化，服务端仍会去重 */
  }
}

// 计算文件 sha256（hex）。不支持 WebCrypto 时返回 null（退化为全部上传）。
async function sha256Hex(file) {
  if (!supported()) return null
  const buf = await file.arrayBuffer()
  const digest = await crypto.subtle.digest('SHA-256', buf)
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

/**
 * 备份一批文件（前台执行）。
 * @param {FileList|File[]} fileList
 * @returns {Promise<{picked:number, skipped:number, queued:number}>}
 */
export async function backupFiles(fileList) {
  const files = Array.from(fileList || [])
  const picked = files.length
  const fresh = []
  const hashByItemId = new Map()
  const seenInBatch = new Set()

  for (const file of files) {
    let hash = null
    try {
      hash = await sha256Hex(file)
    } catch {
      hash = null
    }
    if (hash) {
      if (seenInBatch.has(hash) || (await hasHash(hash))) continue // 批次内/历史重复 → 跳过
      seenInBatch.add(hash)
    }
    fresh.push({ file, hash })
  }

  if (!fresh.length) return { picked, skipped: picked, queued: 0 }

  const items = addFiles(fresh.map((f) => f.file))
  // uploadManager 按入队顺序返回同序 item 数组
  items.forEach((item, i) => {
    const h = fresh[i]?.hash
    if (h) hashByItemId.set(item.id, h)
  })

  // 轮询上传结果：done → 记录 hash；error → 丢弃（下次重试仍会上传）
  const watcher = setInterval(() => {
    for (const [itemId, hash] of hashByItemId) {
      const it = queue.items.find((x) => x.id === itemId)
      if (!it) continue
      if (it.status === 'done') {
        putHash(hash, { name: it.name, size: it.size })
        hashByItemId.delete(itemId)
      } else if (it.status === 'error') {
        hashByItemId.delete(itemId)
      }
    }
    if (hashByItemId.size === 0) clearInterval(watcher)
  }, 1000)

  return { picked, skipped: picked - fresh.length, queued: fresh.length }
}

/**
 * 打开系统文件选择器并备份所选文件。
 * iOS：Safari 会打开"照片"选择器；Android：Chrome 打开照片/文件选择器。
 * 注意：必须由用户手势触发（浏览器要求），且仅在页面前台运行。
 */
export function pickAndBackup() {
  return new Promise((resolve) => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = 'image/*,video/*'
    input.multiple = true
    input.style.display = 'none'
    input.addEventListener('change', async () => {
      document.body.removeChild(input)
      resolve(await backupFiles(input.files))
    })
    document.body.appendChild(input)
    input.click()
  })
}

export function isBackupSupported() {
  return supported()
}

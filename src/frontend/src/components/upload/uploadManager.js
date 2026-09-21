import { reactive } from 'vue'
import http from '../../api/http'

// 分块阈值与块大小：>8MB 走 Content-Range 分块续传
const CHUNK_SIZE = 8 * 1024 * 1024
// 同时上传文件数上限
const MAX_CONCURRENT = 2
// 单块失败重试次数
const CHUNK_RETRY = 3

let seq = 0

// 上传队列（单例，UploadView 使用）
export const queue = reactive({
  items: [],
  active: 0
})

export function addFiles(fileList, opts = {}) {
  // Job000066：opts = { folderPath, albumId }——三个上传入口（顶栏全局/文件夹/相册）复用同一队列
  const added = []
  for (const file of fileList) {
    const item = reactive({
      id: ++seq,
      file,
      name: file.name,
      size: file.size,
      status: 'waiting', // waiting | uploading | done | error
      uploaded: 0, // 已确认写入服务端的字节数
      chunkLoaded: 0, // 当前块已发送（用于进度条/速度）
      speed: 0,
      error: '',
      uploadId: null,
      chunked: file.size > CHUNK_SIZE,
      folderPath: opts.folderPath || '',
      albumId: opts.albumId || ''
    })
    queue.items.push(item)
    added.push(item)
  }
  pump()
  return added
}

export function retryItem(item) {
  item.status = 'waiting'
  item.error = ''
  item.chunkLoaded = 0
  pump()
}

export function clearFinished() {
  for (let i = queue.items.length - 1; i >= 0; i--) {
    if (queue.items[i].status === 'done') queue.items.splice(i, 1)
  }
}

function pump() {
  while (queue.active < MAX_CONCURRENT) {
    const next = queue.items.find((it) => it.status === 'waiting')
    if (!next) return
    queue.active++
    uploadItem(next).finally(() => {
      queue.active--
      pump()
    })
  }
}

// 从 409 RANGE_MISMATCH 响应中解析服务端已收字节数："断点不符：期望起点 X，实际 Y"
function parseExpectedOffset(err) {
  const msg = err?.response?.data?.error?.message || ''
  const m = msg.match(/期望起点\s*(\d+)/)
  return m ? Number(m[1]) : null
}

function postChunk(item, offset, end, onProgress) {
  const fd = new FormData()
  fd.append('file', item.file.slice(offset, end + 1), item.name)
  fd.append('space', 'personal')
  if (item.folderPath) fd.append('folder_path', item.folderPath)
  if (item.albumId) fd.append('album_id', item.albumId)
  if (item.uploadId) fd.append('upload_id', item.uploadId)
  const headers = {}
  if (item.chunked) {
    headers['Content-Range'] = `bytes ${offset}-${end}/${item.size}`
  }
  return http.post('/media/upload', fd, {
    headers,
    timeout: 0, // 大文件分块不走默认 15s 超时
    onUploadProgress: onProgress
  })
}

async function uploadChunkWithRetry(item, offset, end) {
  let attempt = 0
  // 速度统计窗口
  let lastTime = Date.now()
  let lastBytes = 0

  for (;;) {
    try {
      const res = await postChunk(item, offset, end, (e) => {
        item.chunkLoaded = e.loaded || 0
        const now = Date.now()
        const dt = now - lastTime
        if (dt >= 500) {
          item.speed = ((item.chunkLoaded - lastBytes) * 1000) / dt
          lastTime = now
          lastBytes = item.chunkLoaded
        }
      })
      return res.data
    } catch (err) {
      // 断点不符：跳到服务端已收位置继续（同一 upload_id 会话内续传）
      if (err?.response?.status === 409) {
        const expected = parseExpectedOffset(err)
        if (expected !== null && item.uploadId) {
          item.uploaded = expected
          item.chunkLoaded = 0
          return { __resume: expected }
        }
        // 会话不可用：丢弃 upload_id 从头重传
        item.uploadId = null
        item.uploaded = 0
        item.chunkLoaded = 0
        return { __resume: 0 }
      }
      attempt++
      if (attempt > CHUNK_RETRY) throw err
      await new Promise((r) => setTimeout(r, 1000 * attempt))
    }
  }
}

async function uploadItem(item) {
  item.status = 'uploading'
  item.error = ''
  try {
    if (!item.chunked) {
      // 小文件：单请求整文件上传
      await postChunk(item, 0, item.size - 1, (e) => {
        item.uploaded = e.loaded || 0
        item.chunkLoaded = 0
      })
      item.uploaded = item.size
    } else {
      // 分块续传：首块生成 upload_id，后续块按序追加
      let offset = item.uploaded || 0
      while (offset < item.size) {
        const end = Math.min(offset + CHUNK_SIZE, item.size) - 1
        const data = await uploadChunkWithRetry(item, offset, end)
        if (data && data.__resume !== undefined) {
          offset = data.__resume
          continue
        }
        if (data.upload_id) item.uploadId = data.upload_id
        if (data.complete === false) {
          item.uploaded = data.received
          offset = data.received
        } else {
          // 最后一块：{id, status} 入库完成
          item.uploaded = item.size
          offset = item.size
        }
        item.chunkLoaded = 0
      }
    }
    item.speed = 0
    item.status = 'done'
  } catch (err) {
    item.speed = 0
    item.status = 'error'
    item.error = err?.response?.data?.error?.message || '网络错误，上传中断'
  }
}

export function formatSpeed(bytesPerSec) {
  if (!bytesPerSec || bytesPerSec <= 0) return ''
  return formatBytes(bytesPerSec) + '/s'
}

export function formatBytes(n) {
  if (!n && n !== 0) return ''
  const units = ['B', 'KB', 'MB', 'GB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return (i === 0 ? v : v.toFixed(1)) + ' ' + units[i]
}

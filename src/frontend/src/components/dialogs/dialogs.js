// Job000077 统一对话框宿主：confirm / prompt / form / alert 四种，Promise 化。
// 单例模块级状态，由 DialogHost.vue（挂在 App.vue）渲染；任何组件与纯 JS 均可 await 调用，
// 替代 window.prompt/confirm/alert——统一交互、可 data-testid 机器断言、非安全上下文行为一致。
import { reactive } from 'vue'

export const dialogState = reactive({
  current: null,
  queue: []
})

let seq = 0

function enqueue(kind, opts) {
  return new Promise((resolve) => {
    dialogState.queue.push({
      id: ++seq,
      kind, // confirm | prompt | form | alert
      title: opts.title || '',
      text: opts.text || '',
      // form: [{ key, label, placeholder, initial, type?: 'check'(布尔勾选，默认文本), validate?(value, values) -> '' | 错误文案 }]
      fields: opts.fields || null,
      initial: opts.initial ?? '',
      confirmText: opts.confirmText || '确定',
      cancelText: opts.cancelText || '取消',
      danger: !!opts.danger,
      value: opts.initial ?? '',
      values: opts.fields ? Object.fromEntries(opts.fields.map((f) => [f.key, f.initial ?? ''])) : null,
      error: '',
      resolve
    })
    pump()
  })
}

function pump() {
  if (!dialogState.current && dialogState.queue.length) {
    dialogState.current = dialogState.queue.shift()
  }
}

function closeCurrent(result) {
  const cur = dialogState.current
  if (!cur) return
  dialogState.current = null
  cur.resolve(result)
  pump()
}

export const dialogs = {
  // -> Promise<boolean>
  confirm: (opts) => enqueue('confirm', opts),
  // -> Promise<string | null>（取消 = null）
  prompt: (opts) => enqueue('prompt', opts),
  // -> Promise<Record<key, string> | null>
  form: (opts) => enqueue('form', opts),
  // -> Promise<true>（仅确定）
  alert: (opts) => enqueue('alert', typeof opts === 'string' ? { text: opts } : opts)
}

// DialogHost「确定」提交：先跑 validate，未过则错误行内显示、不关闭。
export function dialogSubmit() {
  const cur = dialogState.current
  if (!cur) return
  if (cur.kind === 'confirm' || cur.kind === 'alert') {
    closeCurrent(true)
    return
  }
  if (cur.kind === 'prompt') {
    const v = cur.value ?? ''
    const err = cur.validate ? cur.validate(v) : ''
    if (err) {
      cur.error = err
      return
    }
    closeCurrent(v)
    return
  }
  if (cur.kind === 'form') {
    for (const f of cur.fields) {
      const err = f.validate ? f.validate(cur.values[f.key] ?? '', cur.values) : ''
      if (err) {
        cur.error = err
        return
      }
    }
    closeCurrent({ ...cur.values })
  }
}

// DialogHost「取消」：confirm 回 false，prompt/form 回 null（alert 无取消按钮）。
export function dialogCancel() {
  const cur = dialogState.current
  if (!cur) return
  closeCurrent(cur.kind === 'confirm' ? false : null)
}

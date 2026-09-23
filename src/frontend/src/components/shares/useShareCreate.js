// 创建分享对话框状态机（Job000094：ShareCreateDialog 拆 composable，与 088 逻辑密集同路线）。
// 职责：表单状态（标题/有效期/密码/微信开关）、密码校验、提交编排（payload 组装→createShare→
// created 态）、cancel 上抛。created 成功态视图与剪贴板交互归 ShareCreatedPanel（自持）。
import { computed, reactive, ref } from 'vue'
import { createShare, errMsg, shareLink } from './shareApi'

export const expireOptions = [
  { label: '1 天', days: 1 },
  { label: '7 天', days: 7 },
  { label: '30 天', days: 30 },
  { label: '永久', days: 0 }
]

export function useShareCreate(props, emit) {
  const form = reactive({
    title: props.defaultTitle,
    expireDays: 7,
    password: '',
    isWechat: false
  })

  const submitting = ref(false)
  const submitError = ref('')
  const created = ref(null)

  const passwordError = computed(() => {
    if (!form.password) return ''
    if (form.password.length < 4) return '密码长度需为 4-8 位'
    return ''
  })

  const createdLink = computed(() => (created.value ? shareLink(created.value.token) : ''))

  async function submit() {
    if (submitting.value || passwordError.value) return
    submitting.value = true
    submitError.value = ''
    try {
      const payload = { kind: props.kind, target_id: props.targetId }
      if (form.title) payload.title = form.title
      if (form.expireDays > 0) {
        payload.expire_at = new Date(Date.now() + form.expireDays * 86400000).toISOString()
      }
      if (form.password) payload.password = form.password
      if (form.isWechat) payload.is_wechat = true
      created.value = await createShare(payload)
      emit('created', created.value)
    } catch (e) {
      submitError.value = errMsg(e, '创建分享失败')
    } finally {
      submitting.value = false
    }
  }

  function onCancel() {
    emit('cancel')
  }

  return {
    form, submitting, submitError, created,
    passwordError, createdLink, submit, onCancel
  }
}

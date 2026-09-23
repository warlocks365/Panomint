/* 360Player 陀螺仪子系统：R1-R6 全套自持。
 *
 * R1 双事件源互斥（deviceorientation vs absolute 只注册其一，防相机两朝向高频交替）；
 * R2/R4 平滑：事件回调只写 targetQuat，相机由 rAF 经 engine.onFrame 钩子统一 slerp；
 * R3 α/β/γ 缺失/非有限直接丢弃（`|| 0` 兜底会把「缺失」误当「0° 真实朝向」）；
 * R5 关闭/降级时 syncLonLatFromCamera 逆映射回 lon/lat，rAF 无缝接管不跳变；
 * R6 开启补偿：首个样本建立 calibOffset，开启瞬间零跳变（等价开启即按一次校准）。
 */
import { ref } from 'vue'
import * as THREE from 'three'

export function usePanoGyro({ engine, showToast }) {
  const gyroOn = ref(false)

  const zee = new THREE.Vector3(0, 0, 1)
  const euler = new THREE.Euler()
  const q0 = new THREE.Quaternion()
  const q1 = new THREE.Quaternion(-Math.sqrt(0.5), 0, 0, Math.sqrt(0.5))
  const calibOffset = new THREE.Quaternion()
  const isWeChat = /MicroMessenger/i.test(navigator.userAgent)
  let gyroWatchdog = 0
  let gyroGotData = false
  /* GYRO_SLEW = 每帧向目标朝向逼近的比例，0.2~0.3 兼顾跟手与平滑：越小越平滑、跟随延迟越大 */
  const GYRO_SLEW = 0.25
  const GYRO_WATCHDOG_MS = 1500  // 首轮等待：传感器首次出数可能略慢
  const GYRO_SWAP_MS = 800       // 换源后第二轮等待：另一个事件源注册后通常立即出数
  const targetQuat = new THREE.Quaternion()
  const gyroRawQuat = new THREE.Quaternion()
  const tmpQuat = new THREE.Quaternion()
  let gyroHasTarget = false
  let gyroEventName = ''            // 当前实际监听的事件名（两者只注册其一）
  let gyroTriedFallback = false     // 看门狗是否已尝试换源
  let gyroNeedInitialCalib = false  // 开启后等待首个样本建立补偿（R6）

  function screenOrientationAngle() {
    return (screen.orientation && screen.orientation.angle) || window.orientation || 0
  }
  // 结果写入 out（不新建 Quaternion）：陀螺仪事件每秒数十次，避免持续分配。
  function orientToQuat(out, alpha, beta, gamma) {
    const d = Math.PI / 180
    euler.set(beta * d, alpha * d, -gamma * d, 'YXZ')
    out.setFromEuler(euler)
    out.multiply(q1)
    out.multiply(q0.setFromAxisAngle(zee, -screenOrientationAngle() * d))
    return out
  }
  function onDeviceOrientation(e) {
    const a = e.alpha, b = e.beta, g = e.gamma
    if (a == null || b == null || g == null) return
    if (!Number.isFinite(a) || !Number.isFinite(b) || !Number.isFinite(g)) return
    gyroGotData = true
    orientToQuat(gyroRawQuat, a, b, g)
    if (gyroNeedInitialCalib) {
      gyroNeedInitialCalib = false
      // offset = 当前视角 × 传感器朝向⁻¹，于是 offset × q_raw === 当前视角，开启瞬间零跳变
      calibOffset.copy(engine.getCamera().quaternion).multiply(tmpQuat.copy(gyroRawQuat).invert())
    }
    targetQuat.copy(gyroRawQuat).premultiply(calibOffset)
    gyroHasTarget = true
  }
  // R1 注释见文件头；两个事件交替命中同一 handler 会让相机在「两个朝向」间高频交替（闪动根因）
  function gyroAbsoluteAvailable() {
    return 'ondeviceorientationabsolute' in window
  }
  function attachGyroListener(name) {
    window.addEventListener(name, onDeviceOrientation)
    gyroEventName = name
  }
  function detachGyroListener() {
    if (!gyroEventName) return
    window.removeEventListener(gyroEventName, onDeviceOrientation)
    gyroEventName = ''
  }
  function armGyroWatchdog(ms) {
    clearTimeout(gyroWatchdog)
    gyroWatchdog = setTimeout(() => {
      if (gyroGotData) return
      if (!gyroTriedFallback) {
        // 第一次超时先换成另一个事件源再等一轮（比直接降级保守），仍无有效数据才降级
        gyroTriedFallback = true
        const alt = gyroEventName === 'deviceorientationabsolute' ? 'deviceorientation' : 'deviceorientationabsolute'
        detachGyroListener()
        attachGyroListener(alt)
        armGyroWatchdog(GYRO_SWAP_MS)
        return
      }
      gyroOff(isWeChat ? '微信浏览器未提供陀螺仪数据，已降级为拖拽模式' : '未检测到陀螺仪数据，已降级为拖拽模式')
    }, ms)
  }
  function attachGyroListeners() {
    gyroGotData = false
    gyroTriedFallback = false
    detachGyroListener()
    attachGyroListener(gyroAbsoluteAvailable() ? 'deviceorientationabsolute' : 'deviceorientation')
    armGyroWatchdog(GYRO_WATCHDOG_MS)
  }
  async function enableGyro() {
    if (typeof DeviceOrientationEvent !== 'undefined' && typeof DeviceOrientationEvent.requestPermission === 'function') {
      try {
        const res = await DeviceOrientationEvent.requestPermission()
        if (res !== 'granted') { gyroOff('权限被拒，已降级为拖拽模式'); return }
      } catch { gyroOff('权限请求失败，已降级为拖拽模式'); return }
    }
    attachGyroListeners()
    // R6：置 gyroOn 之前先把插值目标对齐当前视角，「开启瞬间」与「首个样本到达」两时刻视角都连续
    gyroNeedInitialCalib = true
    targetQuat.copy(engine.getCamera().quaternion)
    gyroHasTarget = true
    gyroOn.value = true
  }
  function gyroOff(note) {
    const wasOn = gyroOn.value
    detachGyroListener()
    clearTimeout(gyroWatchdog)
    gyroOn.value = false
    gyroHasTarget = false
    gyroNeedInitialCalib = false
    if (wasOn && engine.getCamera()) engine.syncLonLatFromCamera()
    if (note) showToast(note)
  }
  function toggleGyro() { gyroOn.value ? gyroOff() : enableGyro() }
  function calibrate() {
    if (gyroOn.value && gyroHasTarget) {
      // 校准 = 当前视角重设为当前传感器朝向的参考，与 R6 开启补偿同一语义，按校准本身不移动画面
      calibOffset.copy(engine.getCamera().quaternion).multiply(tmpQuat.copy(gyroRawQuat).invert())
      targetQuat.copy(gyroRawQuat).premultiply(calibOffset)
      gyroHasTarget = true
    } else {
      calibOffset.copy(engine.getCamera().quaternion).invert()
    }
    showToast('已校准视角')
  }

  /* R2/R4：相机统一由 rAF 插值写入（engine 帧钩子），噪声不逐事件直灌相机。
     取舍：跟随引入约 1~2 帧延迟（多数场景眩晕感反而更轻）；GYRO_SLEW 越小越平滑 */
  function gyroFrame(camera) {
    if (!gyroOn.value || !gyroHasTarget || engine.getIsPresenting()) return false
    camera.quaternion.slerp(targetQuat, GYRO_SLEW)
    return true
  }
  engine.onFrame(gyroFrame)

  function destroy() { gyroOff() }

  return { gyroOn, toggleGyro, calibrate, gyroFrame, destroy }
}

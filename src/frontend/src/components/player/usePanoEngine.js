/* 360Player 引擎核心：Three.js 球面渲染生命周期 + 视角状态机 + rAF 心脏。
 *
 * 自持：renderer/scene/camera/球体与 shader 材质/纹理容器/指针事件/lon·lat 视角/
 *      FOV/rAF 循环（帧钩子机制）/resize/完整 dispose。
 * 宿主只创建 containerRef/videoRef（DOM 归属宿主原则）并传入；
 * 陀螺仪/性能监测等经 onFrame(cb) 注册帧钩子——cb(camera) 返回 true 表示
 * 本帧已写入相机（陀螺仪 slerp 分支），引擎跳过默认 lookAt（拖拽视角分支）。
 */
import * as THREE from 'three'

export function usePanoEngine({ containerRef, videoRef, isPhoto, isGyroActive, pokeBars, onFovApplied }) {
  /* 引擎对象（非响应式） */
  let renderer = null, scene = null, camera = null, texture = null
  let sphereGeo = null, sphereMat = null
  let video = null, el = null
  let maxTex = 0
  let disposed = false

  /* 帧钩子（陀螺仪 slerp / 性能 fpsTick 等；返回 true=已处理相机） */
  const frameHooks = []
  function onFrame(cb) { frameHooks.push(cb) }
  function offFrame(cb) { const i = frameHooks.indexOf(cb); if (i >= 0) frameHooks.splice(i, 1) }

  /* 视角状态机（拖拽/捏合） */
  let lon = 0, lat = 0, downX = 0, downY = 0, downLon = 0, downLat = 0
  let dragging = false
  const pointers = new Map()
  let pinchDist = 0

  function setFov(f) {
    camera.fov = Math.max(45, Math.min(100, f))
    camera.updateProjectionMatrix()
    if (onFovApplied) onFovApplied(Math.round(camera.fov))
  }

  /* ---- 指针事件（命名函数以便卸载） ---- */
  function onPointerDown(e) {
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (pointers.size === 1) {
      dragging = true
      downX = e.clientX; downY = e.clientY
      downLon = lon; downLat = lat
    } else if (pointers.size === 2) {
      const [a, b] = [...pointers.values()]
      pinchDist = Math.hypot(a.x - b.x, a.y - b.y)
    }
    el.setPointerCapture(e.pointerId)
  }
  function onPointerMove(e) {
    if (pointers.has(e.pointerId)) {
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
      if (pointers.size === 2) {
        const [a, b] = [...pointers.values()]
        const d = Math.hypot(a.x - b.x, a.y - b.y)
        if (pinchDist > 0) setFov(camera.fov * (pinchDist / d))
        pinchDist = d
      } else if (dragging && !isGyroActive()) {
        // 陀螺仪开启时拖拽不抢视角（原实现语义）
        lon = downLon + (downX - e.clientX) * 0.12
        lat = Math.max(-85, Math.min(85, downLat + (e.clientY - downY) * 0.12))
      }
    }
    pokeBars()
  }
  function onPointerRelease(e) {
    pointers.delete(e.pointerId)
    if (pointers.size === 0) dragging = false
  }
  function onWheel(e) {
    e.preventDefault()
    setFov(camera.fov + e.deltaY * 0.03)
  }

  function onResize() {
    if (!renderer) return
    const w = containerRef.value.clientWidth, h = containerRef.value.clientHeight
    camera.aspect = w / h
    camera.updateProjectionMatrix()
    renderer.setSize(w, h)
  }

  /* R5：相机朝向按 lookAt 逆映射反解回 lon/lat（陀螺仪关闭/降级时 rAF 无缝接管）。
     注视方向 dir = (sinφcosθ, cosφ, sinφ sinθ)，φ=deg2rad(90-lat)，θ=deg2rad(lon) */
  const gyroDir = new THREE.Vector3()
  function syncLonLatFromCamera() {
    gyroDir.set(0, 0, -1).applyQuaternion(camera.quaternion)
    const dy = Math.max(-1, Math.min(1, gyroDir.y))
    lat = Math.max(-89.9, Math.min(89.9, 90 - THREE.MathUtils.radToDeg(Math.acos(dy))))
    const sinPhi = Math.sqrt(Math.max(0, 1 - dy * dy))
    if (sinPhi > 1e-4) lon = THREE.MathUtils.radToDeg(Math.atan2(gyroDir.z, gyroDir.x))
  }

  function init() {
    video = videoRef.value
    const container = containerRef.value
    const w = container.clientWidth, h = container.clientHeight

    renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' })
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
    renderer.setSize(w, h)
    renderer.xr.enabled = true
    container.appendChild(renderer.domElement)
    el = renderer.domElement

    scene = new THREE.Scene()
    camera = new THREE.PerspectiveCamera(75, w / h, 0.1, 1000)

    texture = isPhoto.value ? new THREE.Texture() : new THREE.VideoTexture(video)
    // SphereGeometry 顶部 UV v=1 → 图片/帧顶行；flipY=true 时 v=1 恰为顶行，天顶朝上。
    // （此前视频路径 flipY=false 为合成素材期未暴露的朝向缺陷，已修正）
    texture.flipY = true
    texture.colorSpace = THREE.SRGBColorSpace

    sphereGeo = new THREE.SphereGeometry(500, 64, 32)
    sphereGeo.scale(-1, 1, 1)
    sphereMat = new THREE.ShaderMaterial({
      uniforms: { map: { value: texture } },
      vertexShader: `
        varying vec2 vUv;
        void main() {
          vUv = uv;
          gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
        }`,
      fragmentShader: `
        uniform sampler2D map;
        varying vec2 vUv;
        void main() {
          gl_FragColor = texture2D(map, vUv);
        }`
    })
    scene.add(new THREE.Mesh(sphereGeo, sphereMat))

    maxTex = renderer.capabilities.maxTextureSize

    /* 事件绑定 */
    el.addEventListener('pointerdown', onPointerDown)
    el.addEventListener('pointermove', onPointerMove)
    el.addEventListener('pointerup', onPointerRelease)
    el.addEventListener('pointercancel', onPointerRelease)
    el.addEventListener('wheel', onWheel, { passive: false })
    el.addEventListener('touchstart', pokeBars, { passive: true })
    document.addEventListener('visibilitychange', onVisibilityChange)
    window.addEventListener('resize', onResize)

    renderer.setAnimationLoop(() => {
      // 帧钩子优先（陀螺仪 slerp 返回 true 则跳默认 lookAt）；fpsTick 等钩子返回 false
      let handled = false
      for (let i = 0; i < frameHooks.length; i++) {
        if (frameHooks[i](camera)) handled = true
      }
      if (!handled && !renderer.xr.isPresenting) {
        const phi = THREE.MathUtils.degToRad(90 - lat)
        const theta = THREE.MathUtils.degToRad(lon)
        camera.lookAt(
          500 * Math.sin(phi) * Math.cos(theta),
          500 * Math.cos(phi),
          500 * Math.sin(phi) * Math.sin(theta)
        )
      }
      renderer.render(scene, camera)
    })
  }

  /* 页面隐藏时暂停播放（video 由 stream 域持有，这里经钩子上抛，默认空实现） */
  let onVisibilityHook = () => {}
  function onVisibilityChange() { onVisibilityHook() }
  function setVisibilityHook(cb) { onVisibilityHook = cb }

  function destroy() {
    disposed = true
    if (el) {
      el.removeEventListener('pointerdown', onPointerDown)
      el.removeEventListener('pointermove', onPointerMove)
      el.removeEventListener('pointerup', onPointerRelease)
      el.removeEventListener('pointercancel', onPointerRelease)
      el.removeEventListener('wheel', onWheel)
      el.removeEventListener('touchstart', pokeBars)
    }
    document.removeEventListener('visibilitychange', onVisibilityChange)
    window.removeEventListener('resize', onResize)
    if (renderer) {
      renderer.setAnimationLoop(null)
      if (renderer.xr.isPresenting && renderer.xr.getSession()) {
        renderer.xr.getSession().end().catch(() => {})
      }
      if (sphereGeo) sphereGeo.dispose()
      if (sphereMat) sphereMat.dispose()
      if (texture) texture.dispose()
      renderer.dispose()
      renderer.forceContextLoss()
      renderer.domElement.remove()
      renderer = null
    }
  }

  function setTexture(tex) {
    const old = texture
    texture = tex
    sphereMat.uniforms.map.value = tex
    if (old) old.dispose()
  }

  return {
    init, destroy, setTexture, setFov, syncLonLatFromCamera,
    onFrame, offFrame, setVisibilityHook,
    getCamera: () => camera, getEl: () => el, getVideo: () => video, getRenderer: () => renderer,
    getMaxTex: () => maxTex, isDisposed: () => disposed,
    getIsPresenting: () => !!(renderer && renderer.xr.isPresenting)
  }
}

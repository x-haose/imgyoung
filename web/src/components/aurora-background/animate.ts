import { readPalette, type Palette } from './palette'
import { AuroraRenderer } from './renderer'

/** 开场流动时长:自动开始的动效在 5 秒内停下(WCAG 2.2.2),之后画面静止,只随指针移动。 */
const INTRO_MS = 4500
/** 开场流动的初速度(动画时钟秒 / 真实秒),随进度二次衰减到 0。 */
const INTRO_SPEED = 4
/** 指针视差的缓动时间常数,毫秒。 */
const POINTER_TAU_MS = 500
/** 设备像素比上限:光带是平滑渐变,超过 2 只多耗电不增观感。 */
const MAX_DPR = 2
/** 绘制分辨率相对 CSS 像素×DPR 的比例;平滑渐变由浏览器放大不失真。 */
const RENDER_SCALE = 0.5

/** 动画运行态。 */
type State = {
  time: number // 动画时钟,秒
  startedAt: number // 开场开始时刻(performance.now),0 表示尚未画过第一帧
  last: number // 上一帧时刻,0 表示需要重新计时
  pointer: [number, number] // 缓动后的指针位置
  target: [number, number] // 指针的实际位置
  palette: Palette // 当前主题配色
  reduced: boolean // 用户要求减少动效:不播开场、不跟指针
  raf: number // 待执行的动画帧,0 表示没有
  lost: boolean // 上下文已丢失:不再绘制,也不再请求动画帧
}

/** 推进一帧的时钟与指针缓动。@return 是否还需要下一帧 */
function advance(s: State, now: number): boolean {
  if (s.startedAt === 0) s.startedAt = now
  const dt = s.last ? Math.min(now - s.last, 50) : 16
  s.last = now
  if (s.reduced) return false
  const progress = Math.min((now - s.startedAt) / INTRO_MS, 1)
  s.time += (dt / 1000) * INTRO_SPEED * (1 - progress) ** 2
  const k = 1 - Math.exp(-dt / POINTER_TAU_MS)
  s.pointer = [
    s.pointer[0] + (s.target[0] - s.pointer[0]) * k,
    s.pointer[1] + (s.target[1] - s.pointer[1]) * k,
  ]
  const settled =
    Math.abs(s.target[0] - s.pointer[0]) < 1e-3 &&
    Math.abs(s.target[1] - s.pointer[1]) < 1e-3
  return progress < 1 || !settled
}

/** 按容器尺寸设置画布像素尺寸。 */
function fitCanvas(canvas: HTMLCanvasElement) {
  const ratio = Math.min(window.devicePixelRatio || 1, MAX_DPR) * RENDER_SCALE
  canvas.width = Math.max(1, Math.round(canvas.clientWidth * ratio))
  canvas.height = Math.max(1, Math.round(canvas.clientHeight * ratio))
}

/**
 * 在画布上启动极光背景。
 * @description 静态画面同步绘制(启动、尺寸或主题变化时),只有开场流动与指针缓动未停时才请求动画帧;
 * 页面隐藏时不请求。不支持 WebGL2 时什么都不做,画布保持透明。
 * @returns 停止并释放资源的函数
 */
export function startAurora(canvas: HTMLCanvasElement): () => void {
  const palette = readPalette()
  const renderer = palette ? AuroraRenderer.create(canvas) : null
  if (!palette || !renderer) return () => undefined
  const motion = window.matchMedia('(prefers-reduced-motion: reduce)')
  const s: State = {
    time: 0,
    startedAt: 0,
    last: 0,
    pointer: [0.5, 0.7],
    target: [0.5, 0.7],
    palette,
    reduced: motion.matches,
    raf: 0,
    lost: false,
  }
  const draw = () => {
    if (!s.lost)
      renderer.draw({ time: s.time, pointer: s.pointer, ...s.palette })
  }
  const frame = (now: number) => {
    s.raf = 0
    if (document.hidden || s.lost) return
    const busy = advance(s, now)
    draw()
    if (busy) s.raf = requestAnimationFrame(frame)
    else s.last = 0
  }
  const wake = () => {
    if (!s.raf && !s.lost && !document.hidden)
      s.raf = requestAnimationFrame(frame)
  }
  const stopListening = listen(canvas, s, motion, { draw, wake })
  // 首帧同步画出:静态画面不依赖动画帧(后台标签页、节流时也在)
  fitCanvas(canvas)
  draw()
  wake()
  return () => {
    cancelAnimationFrame(s.raf)
    stopListening()
    renderer.dispose()
  }
}

/** 监听回调需要的两个动作:立即补画一帧;按需请求动画帧。 */
type Actions = { draw: () => void; wake: () => void }

/** 挂上尺寸、主题、指针、可见性与减少动效偏好的监听。@returns 解除全部监听的函数 */
function listen(
  canvas: HTMLCanvasElement,
  s: State,
  motion: MediaQueryList,
  { draw, wake }: Actions
): () => void {
  const onMotion = () => {
    s.reduced = motion.matches
    wake()
  }
  const onPointer = (e: PointerEvent) => {
    if (s.reduced) return
    s.target = [
      e.clientX / window.innerWidth,
      1 - e.clientY / window.innerHeight,
    ]
    wake()
  }
  const onVisibility = () => {
    s.last = 0
    wake()
  }
  // GPU 重置等导致上下文丢失后画布不再可画,隐藏它,露出下层的静态 CSS 背景
  const onContextLost = () => {
    cancelAnimationFrame(s.raf)
    s.lost = true
    canvas.style.visibility = 'hidden'
  }
  // 改画布尺寸会清空画面,紧接着同步补画,不闪黑
  const resize = new ResizeObserver(() => {
    fitCanvas(canvas)
    draw()
  })
  // 主题由 <html> 的 class 切换,监听它比依赖某个 React 状态更可靠(跟随系统时也能捕获)
  const theme = new MutationObserver(() => {
    const next = readPalette()
    if (next) s.palette = next
    draw()
  })
  resize.observe(canvas)
  theme.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['class'],
  })
  motion.addEventListener('change', onMotion)
  window.addEventListener('pointermove', onPointer, { passive: true })
  document.addEventListener('visibilitychange', onVisibility)
  canvas.addEventListener('webglcontextlost', onContextLost)
  return () => {
    resize.disconnect()
    theme.disconnect()
    motion.removeEventListener('change', onMotion)
    window.removeEventListener('pointermove', onPointer)
    document.removeEventListener('visibilitychange', onVisibility)
    canvas.removeEventListener('webglcontextlost', onContextLost)
  }
}

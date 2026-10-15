import type { LinearRGB } from './renderer'

/** 极光配色:取自当前主题的设计令牌。 */
export type Palette = {
  base: LinearRGB // 底色,对应 --sidebar(未登录页面的画布色)
  glowA: LinearRGB // 主光色,对应 --aurora-a
  glowB: LinearRGB // 副光色,对应 --aurora-b
  strength: number // 光带强度,[0, 1]
}

/** 光带强度:暗色底上光色更显,强度收一些;亮色光色本身就浅,强度放一些。 */
const STRENGTH = { dark: 0.42, light: 0.6 }

/** sRGB 分量 [0, 1] 转线性。 */
function toLinear(c: number) {
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
}

/**
 * 把任意 CSS 颜色解析为线性 RGB:借 2D 画布的 fillStyle 解析,令牌换成任何合法写法都能读。
 * @param probe 1×1 的 2D 上下文
 * @param color CSS 颜色文本
 */
function parseColor(probe: CanvasRenderingContext2D, color: string): LinearRGB {
  probe.clearRect(0, 0, 1, 1)
  probe.fillStyle = '#000'
  probe.fillStyle = color
  probe.fillRect(0, 0, 1, 1)
  const [r, g, b] = probe.getImageData(0, 0, 1, 1).data
  return [toLinear(r / 255), toLinear(g / 255), toLinear(b / 255)]
}

/** 按 <html> 上当前生效的主题读取配色;2D 画布不可用时返回 null,主题缺令牌时抛错。 */
export function readPalette(): Palette | null {
  const probe = document
    .createElement('canvas')
    .getContext('2d', { willReadFrequently: true })
  if (!probe) return null
  const root = document.documentElement
  const style = getComputedStyle(root)
  const token = (name: string) => {
    const value = style.getPropertyValue(name).trim()
    // 令牌缺失是主题文件写错,当场失败;静默当成黑色会画出一张错的背景
    if (!value) throw new Error(`主题缺少设计令牌 ${name}`)
    return parseColor(probe, value)
  }
  return {
    base: token('--sidebar'),
    glowA: token('--aurora-a'),
    glowB: token('--aurora-b'),
    strength: root.classList.contains('dark') ? STRENGTH.dark : STRENGTH.light,
  }
}

import { FRAGMENT_SHADER, VERTEX_SHADER } from './shaders'

/** 线性空间的 RGB,各分量 [0, 1]。 */
export type LinearRGB = [number, number, number]

/** 一帧的绘制输入。 */
export type AuroraFrame = {
  time: number // 动画时钟,秒
  pointer: [number, number] // 缓动后的指针位置,[0, 1]²,原点在左下
  base: LinearRGB // 底色
  glowA: LinearRGB // 主光色
  glowB: LinearRGB // 副光色
  strength: number // 光带强度,[0, 1]
}

const UNIFORMS = [
  'uResolution',
  'uTime',
  'uPointer',
  'uBase',
  'uGlowA',
  'uGlowB',
  'uStrength',
] as const

type UniformName = (typeof UNIFORMS)[number]

/** 编译单个着色器;失败返回 null(驱动不支持等,调用方据此放弃 WebGL 绘制)。 */
function compile(gl: WebGL2RenderingContext, type: number, source: string) {
  const shader = gl.createShader(type)
  if (!shader) return null
  gl.shaderSource(shader, source)
  gl.compileShader(shader)
  if (gl.getShaderParameter(shader, gl.COMPILE_STATUS)) return shader
  gl.deleteShader(shader)
  return null
}

/** 编译并链接着色器程序;任一步失败返回 null。 */
function link(gl: WebGL2RenderingContext) {
  const vertex = compile(gl, gl.VERTEX_SHADER, VERTEX_SHADER)
  const fragment = compile(gl, gl.FRAGMENT_SHADER, FRAGMENT_SHADER)
  const program = gl.createProgram()
  if (vertex && fragment) {
    gl.attachShader(program, vertex)
    gl.attachShader(program, fragment)
    gl.linkProgram(program)
  }
  if (vertex) gl.deleteShader(vertex)
  if (fragment) gl.deleteShader(fragment)
  if (vertex && fragment && gl.getProgramParameter(program, gl.LINK_STATUS)) {
    return program
  }
  gl.deleteProgram(program)
  return null
}

/**
 * 极光背景的 WebGL2 绘制器:只负责按给定输入画一帧,不管动画节奏。
 * 浏览器不支持 WebGL2、上下文已丢失或着色器编译失败时 create 返回 null,画布保持透明,露出下层的静态 CSS 背景。
 */
export class AuroraRenderer {
  private readonly gl: WebGL2RenderingContext
  private readonly program: WebGLProgram
  private readonly vao: WebGLVertexArrayObject
  private readonly uniforms: Record<UniformName, WebGLUniformLocation | null>

  private constructor(gl: WebGL2RenderingContext, program: WebGLProgram) {
    this.gl = gl
    this.program = program
    this.vao = gl.createVertexArray()
    this.uniforms = Object.fromEntries(
      UNIFORMS.map((name) => [name, gl.getUniformLocation(program, name)])
    ) as Record<UniformName, WebGLUniformLocation | null>
  }

  /** 在画布上创建绘制器;不支持 WebGL2、上下文已丢失或编译失败时返回 null。 */
  static create(canvas: HTMLCanvasElement): AuroraRenderer | null {
    const gl = canvas.getContext('webgl2', {
      alpha: false,
      antialias: false,
      depth: false,
      stencil: false,
      powerPreference: 'low-power',
    })
    if (!gl || gl.isContextLost()) return null
    const program = link(gl)
    return program ? new AuroraRenderer(gl, program) : null
  }

  /** 按画布当前像素尺寸绘制一帧。 */
  draw(frame: AuroraFrame) {
    const { gl, uniforms: u } = this
    const { width, height } = gl.canvas
    gl.viewport(0, 0, width, height)
    gl.useProgram(this.program)
    gl.bindVertexArray(this.vao)
    gl.uniform2f(u.uResolution, width, height)
    gl.uniform1f(u.uTime, frame.time)
    gl.uniform2f(u.uPointer, frame.pointer[0], frame.pointer[1])
    gl.uniform3fv(u.uBase, frame.base)
    gl.uniform3fv(u.uGlowA, frame.glowA)
    gl.uniform3fv(u.uGlowB, frame.glowB)
    gl.uniform1f(u.uStrength, frame.strength)
    gl.drawArrays(gl.TRIANGLES, 0, 3)
  }

  /**
   * 释放着色器程序与顶点数组。
   * @description 不主动丢弃上下文:同一画布重新挂载(开发模式的双重挂载、热更新)会拿回同一个上下文,
   * 丢弃后就再也画不出来;上下文随画布元素一起回收。
   */
  dispose() {
    this.gl.deleteVertexArray(this.vao)
    this.gl.deleteProgram(this.program)
  }
}

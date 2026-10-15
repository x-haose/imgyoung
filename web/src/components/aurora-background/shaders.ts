/** 顶点着色器:用 gl_VertexID 生成一个覆盖全屏的三角形,不需要顶点缓冲。 */
export const VERTEX_SHADER = `#version 300 es
void main() {
  vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
  gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}`

/**
 * 片元着色器:两条极光光带,中线由分形噪声起伏,下沿最亮、向上指数衰减、向下收得利落,叠一层竖向条纹;
 * 光带停在画面上部,卡片所在的中下部只有底色。颜色在线性空间混合,输出前转回 sRGB
 * 并加 ±0.5/255 抖动消除暗部色带。
 */
export const FRAGMENT_SHADER = `#version 300 es
precision highp float;
uniform vec2 uResolution;
uniform float uTime;
uniform vec2 uPointer;
uniform vec3 uBase;
uniform vec3 uGlowA;
uniform vec3 uGlowB;
uniform float uStrength;
out vec4 outColor;

vec2 grad(vec2 p) {
  vec3 q = fract(vec3(p.xyx) * vec3(0.1031, 0.103, 0.0973));
  q += dot(q, q.yzx + 33.33);
  return fract((q.xx + q.yz) * q.zy) * 2.0 - 1.0;
}

float noise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * f * (f * (f * 6.0 - 15.0) + 10.0);
  float a = dot(grad(i), f);
  float b = dot(grad(i + vec2(1.0, 0.0)), f - vec2(1.0, 0.0));
  float c = dot(grad(i + vec2(0.0, 1.0)), f - vec2(0.0, 1.0));
  float d = dot(grad(i + vec2(1.0, 1.0)), f - vec2(1.0, 1.0));
  return 0.5 + 0.5 * mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
}

float fbm(vec2 p) {
  float v = 0.0;
  float a = 0.5;
  for (int i = 0; i < 3; i++) {
    v += a * noise(p);
    p = p * 2.03 + vec2(1.7, 9.2);
    a *= 0.5;
  }
  return v / 0.875;
}

float ribbon(vec2 p, float y0, float seed, float t) {
  float center = y0 + 0.45 * (fbm(vec2(p.x * 0.6 + t * 0.05, seed)) - 0.5);
  float d = p.y - center;
  float glow = d > 0.0 ? exp(-d / 0.11) : exp(-d * d / 0.0025);
  float streak = 0.25 + 0.75 * pow(fbm(vec2(p.x * 5.0 + seed * 3.0 + t * 0.04, p.y * 0.4)), 1.6);
  return glow * streak;
}

void main() {
  vec2 uv = gl_FragCoord.xy / uResolution;
  float aspect = uResolution.x / uResolution.y;
  vec2 p = vec2((uv.x - 0.5) * aspect, uv.y) + (uPointer - 0.5) * vec2(0.08, 0.04);
  float a = ribbon(p, 0.8, 1.3, uTime);
  float b = ribbon(p + vec2(0.6, 0.0), 0.92, 7.9, uTime * 0.8);
  vec3 color = mix(uBase, uGlowA, clamp(a * uStrength, 0.0, 1.0));
  color = mix(color, uGlowB, clamp(b * uStrength * 0.55, 0.0, 1.0));
  color = pow(clamp(color, 0.0, 1.0), vec3(1.0 / 2.2));
  float dither = fract(sin(dot(gl_FragCoord.xy, vec2(12.9898, 78.233))) * 43758.5453) - 0.5;
  outColor = vec4(color + dither / 255.0, 1.0);
}`

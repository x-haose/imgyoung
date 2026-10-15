/** 站点标识:与管理后台同一枚品牌标(翡翠绿圆角方块内嵌快门光圈线条)+ 项目名。 */
export function Logo() {
  return (
    <span className='inline-flex items-center gap-2 font-semibold'>
      <svg viewBox='0 0 32 32' width='22' height='22' aria-hidden='true'>
        <rect x='1' y='1' width='30' height='30' rx='8' fill='#10b981' />
        <circle cx='16' cy='16' r='8.5' fill='none' stroke='#06251b' strokeWidth='2.4' />
        <path
          d='M16 7.5 L16 16 L22.5 21'
          fill='none'
          stroke='#06251b'
          strokeWidth='2.4'
          strokeLinecap='round'
        />
      </svg>
      ImgYoung
    </span>
  )
}

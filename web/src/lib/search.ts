/**
 * 路由查询参数解析:地址栏可被手改,缺失或不合法的值一律按缺省处理,不抛错也不白屏。
 * 路由文件只许用这里的解析器,不引入 zod:路由定义进入首屏入口块,zod 只该随表单页按需加载。
 * 解析器以 Standard Schema 形态交给 validateSearch,路由据此推断搜索参数类型:
 * 有缺省值的键在页面内读到的是非空值,导航时每个键都可省略。
 */

type Parser<T> = (raw: unknown) => T

type Spec = Record<string, Parser<unknown>>

type Simplify<T> = { [K in keyof T]: T[K] } & {}

/** 页面内读到的参数:解析结果可能为 undefined 的键是可选键,其余为必有键。 */
type Output<S extends Spec> = Simplify<
  {
    [
      K in keyof S as undefined extends ReturnType<S[K]> ? never : K
    ]: ReturnType<S[K]>
  } & {
    [
      K in keyof S as undefined extends ReturnType<S[K]> ? K : never
    ]?: ReturnType<S[K]>
  }
>

/** 导航时可传的参数:每个键都可省略,缺省由解析器补齐。 */
type Input<S extends Spec> = { [K in keyof S]?: ReturnType<S[K]> }

type SearchSchema<S extends Spec> = {
  readonly '~standard': {
    readonly version: 1
    readonly vendor: 'imgyoung'
    readonly validate: (value: unknown) => { value: Output<S> }
    readonly types?: { readonly input: Input<S>; readonly output: Output<S> }
  }
}

/** 按字段规格构造搜索参数的解析器;规格外的键丢弃。 */
export function searchSchema<S extends Spec>(spec: S): SearchSchema<S> {
  const parse = (value: unknown): Output<S> => {
    const search = (value ?? {}) as Record<string, unknown>
    const out: Record<string, unknown> = {}
    for (const key in spec) out[key] = spec[key](search[key])
    return out as Output<S>
  }
  return {
    '~standard': {
      version: 1,
      vendor: 'imgyoung',
      validate: (value) => ({ value: parse(value) }),
    },
  }
}

/** 非空字符串;路由会把纯数字串与 true / false 解析成 number / boolean,这里一律还原成字符串。缺省为 undefined,序列化时不出现在地址栏。 */
export const str =
  (fallback?: string): Parser<string | undefined> =>
  (raw) => {
    if (typeof raw === 'number' || typeof raw === 'boolean') return String(raw)
    return typeof raw === 'string' && raw !== '' ? raw : fallback
  }

/** [min, max] 内的整数;路由把纯数字串解析成 number,字符串形式也接受。 */
export const int =
  (
    fallback: number,
    min = Number.MIN_SAFE_INTEGER,
    max = Number.MAX_SAFE_INTEGER
  ): Parser<number> =>
  (raw) => {
    const n = typeof raw === 'number' ? raw : Number(raw)
    return Number.isInteger(n) && n >= min && n <= max ? n : fallback
  }

/** 枚举值之一;不在集合内回缺省,给了缺省值则结果必有值。 */
export function oneOf<const V extends readonly string[]>(
  values: V,
  fallback: V[number]
): Parser<V[number]>
export function oneOf<const V extends readonly string[]>(
  values: V,
  fallback?: undefined
): Parser<V[number] | undefined>
export function oneOf<const V extends readonly string[]>(
  values: V,
  fallback?: V[number]
): Parser<V[number] | undefined> {
  return (raw) =>
    typeof raw === 'string' && (values as readonly string[]).includes(raw)
      ? (raw as V[number])
      : fallback
}

/** 本地日期 YYYY-MM-DD(日界由页面按本地时区展开);其余视为未设置。 */
export const day = (): Parser<string | undefined> => (raw) => {
  const v = str()(raw)
  return v !== undefined && /^\d{4}-\d{2}-\d{2}$/.test(v) ? v : undefined
}

/** 布尔;路由把 true / false 解析成 boolean,字符串形式也接受。 */
export const bool =
  (fallback?: boolean): Parser<boolean | undefined> =>
  (raw) => {
    if (typeof raw === 'boolean') return raw
    if (raw === 'true') return true
    if (raw === 'false') return false
    return fallback
  }

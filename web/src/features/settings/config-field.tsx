import { cn } from '@/lib/utils'
import { useMessages } from '@/context/locale-provider'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { PasswordInput } from '@/components/password-input'
import { type FieldDef, type FieldKey, type FieldValue } from './config-fields'

type ConfigFieldProps = {
  def: FieldDef
  value: FieldValue
  changed: boolean // 相对基线有改动,标题旁打点
  error?: string // 就地校验失败的提示,优先于说明文字显示
  disabled: boolean
  tokenSet: boolean // 凭证字段的占位文案据此区分已配置与未配置
  onChange: (value: FieldValue) => void
}

/** 运行配置的单个字段:标题、控件与说明 / 错误,控件按字段类型渲染。 */
export function ConfigField({
  def,
  value,
  changed,
  error,
  disabled,
  tokenSet,
  onChange,
}: ConfigFieldProps) {
  const c = useMessages().settings.config
  const text = c.fields[def.key]
  const id = `cfg-${def.key}`
  return (
    <div
      className={cn(
        'grid content-start gap-1.5',
        def.kind === 'widths' && 'sm:col-span-2'
      )}
    >
      <Label htmlFor={id} className='flex items-center gap-2'>
        {text.label}
        {changed && (
          <span
            className='size-1.5 rounded-full bg-primary'
            title={c.edited}
            aria-label={c.edited}
          />
        )}
      </Label>
      <FieldControl
        id={id}
        def={def}
        value={value}
        invalid={error !== undefined}
        disabled={disabled}
        tokenSet={tokenSet}
        onChange={onChange}
      />
      <p
        className={cn(
          'text-xs leading-relaxed',
          error ? 'text-destructive' : 'text-muted-foreground'
        )}
      >
        {error ?? text.hint}
      </p>
    </div>
  )
}

type FieldControlProps = {
  id: string
  def: FieldDef
  value: FieldValue
  invalid: boolean
  disabled: boolean
  tokenSet: boolean
  onChange: (value: FieldValue) => void
}

function FieldControl({
  id,
  def,
  value,
  invalid,
  disabled,
  tokenSet,
  onChange,
}: FieldControlProps) {
  const c = useMessages().settings.config
  const placeholders = c.placeholders as Partial<Record<FieldKey, string>>
  switch (def.kind) {
    case 'int': {
      const unit = def.unit ? c.units[def.unit] : ''
      return (
        <div className='relative'>
          <Input
            id={id}
            type='number'
            inputMode='numeric'
            min={def.min}
            max={def.max}
            step={1}
            value={value === '' ? '' : String(value)}
            disabled={disabled}
            aria-invalid={invalid}
            className={cn(
              // 单位写在框内右侧,隐藏原生步进箭头免得两者叠在一起。
              'font-mono [&::-webkit-inner-spin-button]:m-0 [&::-webkit-inner-spin-button]:[-webkit-appearance:none] [&::-webkit-outer-spin-button]:m-0 [&::-webkit-outer-spin-button]:[-webkit-appearance:none]',
              unit && 'pe-24'
            )}
            onChange={(e) => {
              const n = e.target.valueAsNumber
              onChange(e.target.value === '' || Number.isNaN(n) ? '' : n)
            }}
          />
          {unit && (
            <span className='pointer-events-none absolute inset-y-0 end-3 flex items-center text-xs text-muted-foreground'>
              {unit}
            </span>
          )}
        </div>
      )
    }
    case 'text':
    case 'url':
      return (
        <Input
          id={id}
          type={def.kind === 'url' ? 'url' : 'text'}
          value={String(value)}
          placeholder={placeholders[def.key]}
          disabled={disabled}
          aria-invalid={invalid}
          className='font-mono'
          onChange={(e) => onChange(e.target.value)}
        />
      )
    case 'secret':
      return (
        <PasswordInput
          id={id}
          value={String(value)}
          placeholder={tokenSet ? c.tokenSet : c.tokenUnset}
          autoComplete='new-password'
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
        />
      )
    case 'bool':
      return (
        <div className='flex h-9 items-center'>
          <Switch
            id={id}
            checked={value === true}
            disabled={disabled}
            onCheckedChange={onChange}
          />
        </div>
      )
    case 'select': {
      const labels = (c.options as Record<string, Record<string, string>>)[
        def.key
      ]
      return (
        <Select
          value={String(value)}
          disabled={disabled}
          onValueChange={onChange}
        >
          <SelectTrigger id={id} className='w-full'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {def.options.map((option) => (
              <SelectItem key={option} value={option}>
                {labels?.[option] ?? option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
    }
    case 'widths':
      return (
        <Input
          id={id}
          value={String(value)}
          placeholder={placeholders[def.key]}
          disabled={disabled}
          aria-invalid={invalid}
          className='font-mono'
          onChange={(e) => onChange(e.target.value)}
        />
      )
    case 'formats': {
      const selected = Array.isArray(value) ? value : []
      return (
        <div className='flex h-9 flex-wrap items-center gap-5'>
          {def.options.map((option) => (
            <label
              key={option}
              className='flex items-center gap-2 font-mono text-sm'
            >
              <Checkbox
                checked={selected.includes(option)}
                disabled={disabled}
                aria-invalid={invalid}
                onCheckedChange={(checked) =>
                  onChange(
                    checked === true
                      ? [...selected, option]
                      : selected.filter((f) => f !== option)
                  )
                }
              />
              {option}
            </label>
          ))}
        </div>
      )
    }
  }
}

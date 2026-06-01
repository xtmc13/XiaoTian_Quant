/**
 * ValidationBanner — displays code quality hints from static analysis.
 */
import { AlertCircle, Info, AlertTriangle } from 'lucide-react'
import type { ValidationHint } from '@/lib/indicatorContract'
import { cn } from '@/lib/utils'

interface ValidationBannerProps {
  hints: ValidationHint[]
  className?: string
}

export function ValidationBanner({ hints, className }: ValidationBannerProps) {
  if (hints.length === 0) return null

  const errors = hints.filter((h) => h.severity === 'error')
  const warns = hints.filter((h) => h.severity === 'warn')
  const infos = hints.filter((h) => h.severity === 'info')

  return (
    <div className={cn('space-y-1.5', className)}>
      {errors.map((h, i) => (
        <HintRow key={`e-${i}`} hint={h} icon={AlertCircle} colorClass="border-red-500/20 bg-red-500/10 text-red-400" />
      ))}
      {warns.map((h, i) => (
        <HintRow key={`w-${i}`} hint={h} icon={AlertTriangle} colorClass="border-amber-500/20 bg-amber-500/10 text-amber-400" />
      ))}
      {infos.map((h, i) => (
        <HintRow key={`i-${i}`} hint={h} icon={Info} colorClass="border-blue-500/20 bg-blue-500/10 text-blue-400" />
      ))}
    </div>
  )
}

function HintRow({
  hint,
  icon: Icon,
  colorClass,
}: {
  hint: ValidationHint
  icon: React.ComponentType<{ className?: string }>
  colorClass: string
}) {
  const text = hintToText(hint)
  return (
    <div className={cn('flex items-start gap-1.5 rounded-lg border px-2.5 py-1.5 text-[10px]', colorClass)}>
      <Icon className="mt-0.5 h-3 w-3 shrink-0" />
      <span className="leading-tight">{text}</span>
    </div>
  )
}

function hintToText(hint: ValidationHint): string {
  const p = hint.params || {}
  switch (hint.code) {
    case 'EMPTY_CODE':
      return '代码为空'
    case 'MISSING_OUTPUT':
      return '缺少 output 字典。指标代码必须定义 output = {plots: [...], signals: [...]}'
    case 'MISSING_INDICATOR_NAME':
      return '缺少 my_indicator_name 变量声明'
    case 'MISSING_INDICATOR_DESCRIPTION':
      return '缺少 my_indicator_description 变量声明'
    case 'MISSING_BUY_SELL_COLUMNS':
      return '缺少 df["buy"] 或 df["sell"] 信号列'
    case 'MISSING_DF_COPY':
      return '建议添加 df = df.copy() 以避免修改原始数据'
    case 'DECLARED_PARAMS_NOT_READ_VIA_PARAMS_GET':
      return `已声明的参数未通过 params.get(...) 读取：${(p.names || []).join('、')}`
    case 'FUTURE_DATA_LEAK':
      return `检测到未来数据泄露：${p.snippet || '?'}。回测会用到尚未发生的K线，请改用 .shift(N) 正数或 iloc[i-N] 引用过去。`
    case 'NDARRAY_PANDAS_METHOD_MISUSE':
      return `在 ndarray 上调用了 pandas 方法：${p.symbol}.${p.method}(...)。请用 pd.Series(arr, index=df.index) 包装回 Series。`
    case 'UNSAFE_IMPORT':
      return `检测到不安全的模块导入：${(p.modules || []).join('、')}`
    case 'NO_STOP_AND_TAKE_PROFIT':
      return '有交易信号但未声明止损和止盈默认配置（# @strategy stopLossPct / takeProfitPct）'
    case 'NO_STOP_LOSS':
      return '未声明止损默认配置（# @strategy stopLossPct）'
    case 'NO_TAKE_PROFIT':
      return '未声明止盈默认配置（# @strategy takeProfitPct）'
    case 'UNKNOWN_STRATEGY_KEY':
      return `存在未知的 @strategy 键：${p.key}`
    case 'RUNTIME_ERROR_ON_VERIFY':
      return `运行时错误：${p.detail || p.error_type || 'Unknown'}`
    default:
      return `提示：${hint.code}`
  }
}

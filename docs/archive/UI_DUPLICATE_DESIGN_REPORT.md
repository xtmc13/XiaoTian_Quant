# 小天量化前端 — 重复设计审查报告

---

## 🔴 严重重复 (必须重构)

### 1. 空状态组件 — 4个文件完全相同的结构

**涉及文件**:
- `components/bots/SignalExecutorPanel.tsx` (出现2次: 信号源空状态 + 持仓空状态)
- `components/bots/AIRobotPanel.tsx` (出现1次: AI信号空状态)
- `components/trading/ContractPanel.tsx` (出现1次: 保证金空状态)

**重复代码**:
```tsx
<div className="text-center py-8 text-[#555]">
  <XXXIcon className="w-8 h-8 mx-auto mb-2 opacity-50" />
  <p className="text-sm">暂无XXX</p>
</div>
```

**问题**: 4个地方复制粘贴相同的空状态结构，只有图标和文案不同
**修复**: 提取 `EmptyState` 组件
```tsx
// components/ui/EmptyState.tsx
interface EmptyStateProps {
  icon: React.ReactNode
  title: string
  description?: string
  action?: React.ReactNode
}

export const EmptyState: React.FC<EmptyStateProps> = ({ icon, title, description, action }) => (
  <div className="text-center py-8 text-[#555]">
    <div className="w-8 h-8 mx-auto mb-2 opacity-50">{icon}</div>
    <p className="text-sm">{title}</p>
    {description && <p className="text-xs text-[#666] mt-1">{description}</p>}
    {action && <div className="mt-4">{action}</div>}
  </div>
)
```

---

### 2. KPI卡片 — 3个文件重复相同的4格布局

**涉及文件**:
- `components/bots/SignalExecutorPanel.tsx` — ExecutorKPI
- `components/bots/AIRobotPanel.tsx` — AIStatusCards
- `components/trading/ContractPanel.tsx` — ContractStatusCards

**重复代码** (每个文件都有):
```tsx
// 1. Loading态
<div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
  {Array.from({ length: 4 }).map((_, i) => (
    <Skeleton key={i} className="h-20 rounded-xl" />
  ))}
</div>

// 2. 数据态
<div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
  {items.map((item) => (
    <div className="rounded-xl border border-[#1c1c1c] bg-[#111] p-4">
      <div className="flex items-center gap-2 mb-2">
        {item.icon}
        <span className="text-xs text-[#888]">{item.label}</span>
      </div>
      <div className="text-lg font-semibold text-[#e0e0e0]">{item.value}</div>
    </div>
  ))}
</div>
```

**问题**: 3个组件内部都定义了完全相同的KPI子组件，结构完全一致
**修复**: 提取 `KPICard` 和 `KPIGrid` 通用组件
```tsx
// components/ui/KPICard.tsx
interface KPICardProps {
  label: string
  value: string | number
  icon: React.ReactNode
  variant?: 'default' | 'success' | 'error' | 'warning' | 'info'
}

export const KPICard: React.FC<KPICardProps> = ({ label, value, icon, variant = 'default' }) => (
  <div className="rounded-xl border border-[#1c1c1c] bg-[#111] p-4">
    <div className="flex items-center gap-2 mb-2">
      {icon}
      <span className="text-xs text-[#888]">{label}</span>
    </div>
    <div className="text-lg font-semibold text-[#e0e0e0]">{value}</div>
  </div>
)

// components/ui/KPIGrid.tsx
interface KPIGridProps {
  items: KPICardProps[]
  isLoading?: boolean
}

export const KPIGrid: React.FC<KPIGridProps> = ({ items, isLoading }) => {
  if (isLoading) {
    return (
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        {Array.from({ length: items.length }).map((_, i) => (
          <Skeleton key={i} className="h-20 rounded-xl" />
        ))}
      </div>
    )
  }
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      {items.map((item) => <KPICard key={item.label} {...item} />)}
    </div>
  )
}
```

---

### 3. 列表项样式 — 2个文件重复相同的行布局

**涉及文件**:
- `components/bots/SignalExecutorPanel.tsx` — ExecutionRecordsView
- `components/trading/TradeHistory.tsx` — 交易历史行

**重复代码**:
```tsx
<div className="flex items-center justify-between rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2">
  <div className="flex items-center gap-2">
    <Badge variant={...}>{...}</Badge>
    <span className="text-xs text-[#ccc]">{symbol}</span>
  </div>
  <div className="flex items-center gap-3">
    <span className="text-xs text-[#888]">@{price}</span>
    <span className={cn('text-xs', pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
      {pnl}
    </span>
  </div>
</div>
```

**修复**: 提取 `TradeRow` 通用组件

---

## 🟡 中度重复 (建议重构)

### 4. 三态模式 — 5个子组件重复 loading/empty/data

**涉及文件**: `SignalExecutorPanel.tsx` 内部的5个子组件

每个子组件都有完全相同的三态结构:
```
if (isLoading) return <Skeleton />
if (!data || data.length === 0) return <EmptyState />
return <DataView data={data} />
```

出现位置:
1. `ExecutorKPI` — loading → skeleton grid / data → KPI cards
2. `TPStats` — loading → skeleton / data → stat grid
3. `SignalSourcesView` — loading → skeleton / empty → empty state / data → list
4. `ActivePositionsView` — loading → skeleton / empty → empty state / data → expandable list
5. `ExecutionRecordsView` — loading → skeleton / empty → empty state / data → scrollable list

**修复**: 提取 `AsyncDataWrapper` HOC
```tsx
// components/ui/AsyncDataWrapper.tsx
interface AsyncDataWrapperProps<T> {
  isLoading: boolean
  data: T | undefined
  emptyState: React.ReactNode
  skeleton: React.ReactNode
  children: (data: T) => React.ReactNode
}

export function AsyncDataWrapper<T>({ isLoading, data, emptyState, skeleton, children }: AsyncDataWrapperProps<T>) {
  if (isLoading) return <>{skeleton}</>
  if (!data || (Array.isArray(data) && data.length === 0)) return <>{emptyState}</>
  return <>{children(data)}</>
}
```

---

### 5. SectionCard 包裹 — 每个面板都有4-5个重复包裹

**涉及文件**:
- `SignalExecutorPanel.tsx` — 4个 SectionCard
- `ContractPanel.tsx` — 4个 SectionCard
- `AIRobotPanel.tsx` — 3个 SectionCard
- `StrategyConfigPanel.tsx` — 5个 SectionCard

每个都是:
```tsx
<SectionCard title="XXX">
  <SomeContent />
</SectionCard>
```

**问题**: SectionCard 本身很简单，但5个面板×4个section = 20次重复包裹
**修复**: 可以接受，但如果需要统一添加"展开/收起"功能时，需要改20个地方

---

### 6. 颜色变体映射 — 3个文件重复相同的映射表

**涉及文件**:
- `SignalExecutorPanel.tsx` — typeLabels, typeVariants
- `components/bots/BotList.tsx` — 可能有状态颜色映射
- `pages/Bots.tsx` — STATUS_META

**重复模式**:
```tsx
const typeLabels: Record<string, string> = { entry: '开仓', tp1: 'TP1', ... }
const typeVariants: Record<string, BadgeVariant> = { entry: 'info', tp1: 'success', ... }
```

**修复**: 统一到一个常量文件 `lib/constants.ts`

---

## 🟢 轻微重复 (可接受)

### 7. 时间格式化 — 2处重复
```tsx
new Date(rec.executed_at).toLocaleTimeString()
new Date(status.updated_at).toLocaleTimeString()
```

### 8. 金额格式化判断颜色 — 多处重复
```tsx
// 出现N次
className={pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'}
```
已有 `formatCurrency` 但没有带颜色的版本

---

## 重复设计统计

| 重复类型 | 出现次数 | 涉及文件数 | 重构收益 |
|----------|----------|------------|----------|
| 空状态组件 | 4 | 3 | 高 |
| KPI卡片 | 3 | 3 | 高 |
| 列表项行 | 2 | 2 | 中 |
| 三态模式 | 5 | 1 | 高 |
| SectionCard包裹 | 20+ | 4 | 低 |
| 颜色映射表 | 3 | 3 | 中 |

---

## 建议重构优先级

```
P0: 提取 EmptyState + KPICard + KPIGrid (3个组件, 消除9处重复)
P1: 提取 AsyncDataWrapper HOC (消除5处三态重复)
P1: 提取 TradeRow 通用列表项 (消除2处重复)
P2: 统一颜色映射常量 (消除3处重复)
P2: 添加 formatCurrencyWithColor 工具函数
```

**预估重构工作量: 2-3小时**

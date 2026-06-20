# 小天量化前端 UI 审查报告

## 严重问题 (P0) — 必须修复

### 1. 图标重复导入导致编译错误
**文件**: `web/src/pages/Bots.tsx`
**行号**: 第6行 + 第8行
**问题**:
```typescript
// 第6行已导入
import { Bot, TrendingUp, TrendingDown, ... } from 'lucide-react'
// 第8行又导入一次（别名）
import { Settings2, TrendingUp as TrendingUpIcon } from 'lucide-react'
```
**影响**: TypeScript 编译报错，无法构建
**修复**: 合并到一次导入
```typescript
import { Bot, TrendingUp, TrendingDown, ... , Settings2 } from 'lucide-react'
```

---

## 中等问题 (P1) — 建议修复

### 2. 颜色硬编码 — 不支持主题切换
**文件**: 全部新组件
**问题**: 使用大量 Tailwind 任意值硬编码颜色
```typescript
text-[#52c41a]     // 绿色
text-[#f5222d]     // 红色
text-[#1890ff]     // 蓝色
text-[#faad14]     // 黄色
bg-[#f5222d]/5     // 背景色
border-[#faad14]/40 // 边框色
```
**影响**: 
- 无法切换主题（暗黑/明亮）
- 品牌色变更需全局搜索替换
- 与设计系统脱节
**修复**: 使用 CSS 变量或 Tailwind 配置的主题色
```css
/* globals.css */
:root {
  --color-success: #52c41a;
  --color-danger: #f5222d;
  --color-primary: #1890ff;
  --color-warning: #faad14;
}
```
```typescript
// 改为
text-[var(--color-success)]
```

### 3. 字体过小 (10px)
**文件**: `StrategyConfigPanel.tsx` 第160行
```typescript
<div className="text-[10px] text-[#666]">倍投补仓 2,4,8,16,32,64</div>
```
**影响**: 可读性差，WCAG 标准要求最小 12px
**修复**: 改为 `text-xs` (12px) 或 `text-[11px]`

### 4. Slider 组件类型不匹配
**文件**: `StrategyConfigPanel.tsx` 多处
**问题**: 
```typescript
<Slider
  value={addPositionSpread}
  onChange={(v) => setValue('add_position_spread', v)}
/>
```
**影响**: `setValue` 期望 `number`，Slider 的 `onChange` 可能传 `ChangeEvent`，类型不匹配
**修复**: 确保 Slider 组件 onChange 回调传 `number` 而非 event

### 5. 缺少错误边界 (Error Boundary)
**文件**: 所有新组件
**问题**: 没有 `react-error-boundary` 或自定义错误边界
**影响**: API 失败或运行时错误会导致整个页面白屏
**修复**: 在 Bots.tsx 页面级添加 ErrorBoundary
```typescript
import { ErrorBoundary } from 'react-error-boundary'

<ErrorBoundary fallback={<ErrorFallback />}>
  {activeTab === 'signal' && <SignalExecutorPanel />}
</ErrorBoundary>
```

---

## 低优先级 (P2) — 体验优化

### 6. 空状态缺少引导操作
**文件**: `SignalExecutorPanel.tsx`, `AIRobotPanel.tsx`
**问题**: 空状态只有文案和图标，没有"创建"按钮
```typescript
// 当前
<div className="text-center py-8 text-[#555]">
  <Radio className="w-8 h-8 mx-auto mb-2 opacity-50" />
  <p className="text-sm">暂无信号来源配置</p>
</div>
```
**修复**: 添加引导按钮
```typescript
<div className="text-center py-8 text-[#555]">
  <Radio className="w-8 h-8 mx-auto mb-2 opacity-50" />
  <p className="text-sm mb-4">暂无信号来源配置</p>
  <Button variant="primary" size="sm">创建信号源</Button>
</div>
```

### 7. 表单缺少 Zod Schema 验证
**文件**: `StrategyConfigPanel.tsx`, `ContractPanel.tsx`, `AIRobotPanel.tsx`
**问题**: 仅用 register 的 inline validation，复杂校验难以维护
```typescript
{...register('leverage', {
  min: { value: 1, message: '最小1x' },
  max: { value: 125, message: '最大125x' },
})}
```
**修复**: 使用 zod + @hookform/resolvers
```typescript
import { z } from 'zod'
import { zodResolver } from '@hookform/resolvers/zod'

const schema = z.object({
  leverage: z.number().min(1).max(125),
  // ...
})
```

### 8. 缺少骨架屏渐变动画
**文件**: `SignalExecutorPanel.tsx`, `ContractPanel.tsx`
**问题**: Skeleton 组件是静态灰色块，没有 pulse/shimmer 动画
**修复**: 添加 animate-pulse
```typescript
<Skeleton className="h-20 rounded-xl animate-pulse" />
```

### 9. KPI 卡片在小屏幕信息拥挤
**文件**: `SignalExecutorPanel.tsx` 第34行, `ContractPanel.tsx`
**问题**: `grid-cols-2 sm:grid-cols-4` 在 375px 屏幕上只有 ~180px 宽度
**修复**: 小屏幕改为单列或两列加大间距
```typescript
// 改为
<div className="grid grid-cols-1 xs:grid-cols-2 sm:grid-cols-4 gap-3">
```

### 10. 时间格式化缺少本地化
**文件**: `SignalExecutorPanel.tsx` 第372行
```typescript
new Date(rec.executed_at).toLocaleTimeString()
```
**修复**: 指定 locale 和选项
```typescript
new Date(rec.executed_at).toLocaleTimeString('zh-CN', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})
```

---

## 代码规范问题

### 11. Props 命名不一致
- 有些组件用 `isLoading`，有些可能用 `loading`
- 建议统一：`isLoading`

### 12. 缺少 React.memo 优化
**文件**: 所有列表项组件
**问题**: `StrategyCard`, `ExecutionRecordsView` 等列表项没有 memo
**影响**: 父组件状态更新时全部重新渲染
**修复**:
```typescript
const StrategyCard = React.memo<StrategyCardProps>((props) => {
  // ...
})
```

### 13. useCallback 缺失
**文件**: `Bots.tsx`, `SignalExecutorPanel.tsx`
**问题**: 事件处理器没有 useCallback 包裹
**影响**: 子组件不必要的重渲染
**修复**:
```typescript
const handleStart = useCallback((bot: BotItem) => {
  startBot(bot)
}, [startBot])
```

---

## 总结

| 优先级 | 问题数 | 修复工作量 |
|--------|--------|------------|
| P0 (严重) | 1 | 5分钟 |
| P1 (中等) | 5 | 2小时 |
| P2 (优化) | 7 | 3小时 |
| **总计** | **13** | **~5小时** |

**建议修复顺序**: P0 → P1.5 → P1.2 → P1.4 → P1.1 → P1.3 → P2

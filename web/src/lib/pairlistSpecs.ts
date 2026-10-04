import type { PairlistComponentSpec, PairlistParamSpec } from '@/lib/api'

/** 与 PairlistManagement 页模板同构的最小结构（避免反向依赖页面组件）。 */
export interface SpecFieldDef {
  key: string
  label: string
  type: 'tags' | 'number' | 'text' | 'select' | 'bool'
  min?: number
  max?: number
  step?: number
  options?: { value: string; label: string }[]
  placeholder?: string
}

export interface SpecTemplate {
  name: string
  label: string
  description: string
  defaultParams: Record<string, unknown>
  fields: SpecFieldDef[]
}

export interface MergedSpecs {
  templates: SpecTemplate[]
  /** 后端规格中存在的组件名（可用于"后端支持"标识）。 */
  backendNames: Set<string>
}

function fieldFromParam(p: PairlistParamSpec): SpecFieldDef {
  const base: SpecFieldDef = {
    key: p.key,
    label: p.label || p.key,
    type: p.type === 'tags' || p.type === 'select' || p.type === 'bool' || p.type === 'text' ? p.type : 'number',
  }
  if (p.min != null) base.min = p.min
  if (p.max != null) base.max = p.max
  if (p.step != null) base.step = p.step
  if (p.options && p.options.length > 0) {
    base.type = 'select'
    base.options = p.options.map((o) => ({ value: o, label: o }))
  }
  return base
}

function templateFromSpec(spec: PairlistComponentSpec): SpecTemplate {
  const defaultParams: Record<string, unknown> = {}
  for (const p of spec.params ?? []) {
    if (p.default !== undefined) defaultParams[p.key] = p.default
  }
  return {
    name: spec.name,
    label: spec.label || spec.name,
    description: spec.description || '',
    defaultParams,
    fields: (spec.params ?? []).map(fieldFromParam),
  }
}

/**
 * 本地内置模板与后端 /pairlist/specs 规格合并：
 * - 后端规格存在且本地也有 → 保留本地模板（字段更全），标记 backend；
 * - 后端规格有、本地没有 → 由规格生成通用模板（新组件即时可用）；
 * - 本地有、后端规格没有 → 保留（规格清单是能力子集，工厂实际支持更多），不标记。
 * specs 为空/请求失败 → 原样返回本地清单。
 */
export function mergeTemplatesWithSpecs<T extends SpecTemplate>(local: T[], specs?: PairlistComponentSpec[] | null): {
  templates: T[]
  backendNames: Set<string>
} {
  if (!specs || specs.length === 0) {
    return { templates: local, backendNames: new Set() }
  }
  const backendNames = new Set(specs.map((s) => s.name))
  const byName = new Map<string, T>(local.map((t) => [t.name, t]))
  const merged: T[] = [...local]
  for (const spec of specs) {
    if (!byName.has(spec.name)) {
      merged.push(templateFromSpec(spec) as T)
    }
  }
  return { templates: merged, backendNames }
}

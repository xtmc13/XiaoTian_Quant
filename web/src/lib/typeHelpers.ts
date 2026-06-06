/**
 * Type-safe data extraction helpers.
 * These functions normalize backend responses that may be wrapped in { data: ... }
 * or returned as raw arrays/objects.
 */

/**
 * Extract an array from a possibly-wrapped API response.
 * Handles: { data: T[] }, { result: T[] }, { list: T[] }, T[] (raw array)
 */
export function extractArray<T>(
  response: unknown,
  ...keys: string[]
): T[] {
  if (response == null) return []
  if (Array.isArray(response)) return response as T[]

  const obj = response as Record<string, unknown>
  const preferredKeys = keys.length > 0 ? keys : ['data', 'result', 'list', 'items']
  for (const key of preferredKeys) {
    const val = obj[key]
    if (Array.isArray(val)) return val as T[]
  }
  return []
}

/**
 * Extract a single object from a possibly-wrapped API response.
 * Handles: { data: T }, T (raw object)
 */
export function extractObject<T extends Record<string, unknown>>(
  response: unknown,
  ...keys: string[]
): T | null {
  if (response == null) return null
  if (typeof response !== 'object') return null

  const obj = response as Record<string, unknown>
  const preferredKeys = keys.length > 0 ? keys : ['data', 'result']
  for (const key of preferredKeys) {
    const val = obj[key]
    if (val != null && typeof val === 'object' && !Array.isArray(val)) {
      return val as T
    }
  }
  // If no wrapper key found, return the object itself if it's not an array
  if (!Array.isArray(obj)) return obj as T
  return null
}

/**
 * Safely read a numeric value from an object.
 */
export function safeNumber(value: unknown, fallback = 0): number {
  if (typeof value === 'number') return value
  if (typeof value === 'string') {
    const n = parseFloat(value)
    return isNaN(n) ? fallback : n
  }
  return fallback
}

/**
 * Safely read a string value from an object.
 */
export function safeString(value: unknown, fallback = ''): string {
  if (typeof value === 'string') return value
  if (value == null) return fallback
  return String(value)
}

/**
 * Type guard: check if value is a Record<string, unknown>.
 */
export function isObject(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value)
}

/**
 * Type guard: check if value is an array.
 */
export function isArray<T>(value: unknown): value is T[] {
  return Array.isArray(value)
}

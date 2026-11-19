function createStorage(): Storage {
  const entries = new Map<string, string>()
  return {
    get length() {
      return entries.size
    },
    clear() {
      entries.clear()
    },
    getItem(key) {
      return entries.get(String(key)) ?? null
    },
    key(index) {
      return Array.from(entries.keys())[index] ?? null
    },
    removeItem(key) {
      entries.delete(String(key))
    },
    setItem(key, value) {
      entries.set(String(key), String(value))
    },
  }
}

Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: createStorage(),
})
Object.defineProperty(globalThis, 'sessionStorage', {
  configurable: true,
  value: createStorage(),
})

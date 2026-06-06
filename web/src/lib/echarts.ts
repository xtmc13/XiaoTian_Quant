/* ── ECharts lazy load ───────────────────────────────────────────── */
let echartsLib: typeof import('echarts') | null = null

export async function getEcharts() {
  if (!echartsLib) {
    echartsLib = await import('echarts')
  }
  return echartsLib
}

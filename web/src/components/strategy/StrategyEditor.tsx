import { useState } from 'react'
import { aiApi } from '@/lib/api'
import { FileCode2, RotateCcw, Save, BrainCircuit } from 'lucide-react'

const DEFAULT_CODE = `from freqtrade.strategy import IStrategy
import talib.abstract as ta

class MyStrategy(IStrategy):
    timeframe = '15m'
    minimal_roi = {"0": 0.01, "60": 0.005}
    stoploss = -0.40

    def populate_indicators(self, dataframe, metadata):
        dataframe['ema_short'] = ta.EMA(dataframe, timeperiod=12)
        dataframe['ema_long'] = ta.EMA(dataframe, timeperiod=26)
        return dataframe

    def populate_entry_trend(self, dataframe, metadata):
        dataframe.loc[dataframe['ema_short'] > dataframe['ema_long'], 'enter_long'] = 1
        return dataframe

    def populate_exit_trend(self, dataframe, metadata):
        dataframe.loc[dataframe['ema_short'] < dataframe['ema_long'], 'exit_long'] = 1
        return dataframe`

export function StrategyEditor() {
  const [code, setCode] = useState(DEFAULT_CODE)
  const [aiPrompt, setAiPrompt] = useState('')
  const [aiResponse, setAiResponse] = useState('')
  const [generating, setGenerating] = useState(false)

  const handleGenerate = async () => {
    if (!aiPrompt.trim()) return
    setGenerating(true)
    try {
      const res = await aiApi.generate({ prompt: aiPrompt })
      setAiResponse(res?.strategy_code || res?.explanation || 'AI 建议将显示在这里...')
    } catch {
      setAiResponse('生成失败，请稍后重试')
    } finally {
      setGenerating(false)
    }
  }

  return (
    <div className="h-full flex">
      <div className="flex-1 flex flex-col min-w-0">
        <div className="flex items-center justify-between px-4 py-2 border-b border-quant-border bg-quant-bg-secondary">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <FileCode2 className="w-3.5 h-3.5" />
            Python 策略代码
          </div>
          <div className="flex gap-2">
            <button onClick={() => setCode(DEFAULT_CODE)} className="px-2.5 py-1.5 rounded-md bg-quant-bg border border-quant-border text-[10px] hover:bg-quant-hover transition-colors flex items-center gap-1">
              <RotateCcw className="w-3 h-3" /> 重置
            </button>
            <button className="px-2.5 py-1.5 rounded-md bg-quant-gold/10 border border-quant-gold/20 text-quant-gold text-[10px] hover:bg-quant-gold/20 transition-colors flex items-center gap-1">
              <Save className="w-3 h-3" /> 保存
            </button>
          </div>
        </div>
        <textarea
          value={code}
          onChange={(e) => setCode(e.target.value)}
          className="flex-1 bg-quant-bg p-4 font-mono text-[11px] leading-relaxed resize-none focus:outline-none border-none"
          spellCheck={false}
        />
      </div>

      <div className="hidden md:flex w-80 shrink-0 border-l border-quant-border bg-quant-bg-secondary flex-col">
        <div className="px-4 py-3 border-b border-quant-border text-xs font-semibold flex items-center gap-2">
          <BrainCircuit className="w-4 h-4 text-quant-gold" /> AI 策略助手
        </div>
        <div className="p-3 space-y-3">
          <textarea
            value={aiPrompt}
            onChange={(e) => setAiPrompt(e.target.value)}
            placeholder="描述你的交易思路，AI 将生成策略代码..."
            className="w-full h-24 bg-quant-bg border border-quant-border rounded-lg p-3 text-xs resize-none focus:outline-none focus:border-quant-gold"
          />
          <button onClick={handleGenerate} disabled={generating} className="w-full py-2 bg-quant-gold text-white rounded-lg text-xs font-medium hover:opacity-90 disabled:opacity-50 transition-opacity">
            {generating ? '生成中...' : '生成策略'}
          </button>
        </div>
        <div className="flex-1 overflow-y-auto px-3 pb-3">
          <div className="rounded-lg border border-quant-border bg-quant-card p-3 text-xs text-muted-foreground whitespace-pre-wrap">
            {aiResponse || 'AI 建议将显示在这里...'}
          </div>
        </div>
      </div>
    </div>
  )
}

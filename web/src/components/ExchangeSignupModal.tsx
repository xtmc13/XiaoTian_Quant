import { useState } from 'react'
import { X, ExternalLink, CheckCircle2 } from 'lucide-react'
import { cn } from '@/lib/utils'

interface ExchangeInfo {
  name: string
  logo: string
  referralUrl: string
  benefits: string[]
}

const FALLBACK_EXCHANGES: ExchangeInfo[] = [
  {
    name: 'Binance',
    logo: 'BN',
    referralUrl: 'https://accounts.binance.com/register?ref=',
    benefits: ['全球最大交易所', '最低手续费 0.1%', '1200+ 交易对', '支持现货/合约'],
  },
  {
    name: 'OKX',
    logo: 'OK',
    referralUrl: 'https://www.okx.com/join/',
    benefits: ['顶级衍生品交易所', '统一账户模式', 'Web3 钱包集成', '支持现货/合约/期权'],
  },
  {
    name: 'Bybit',
    logo: 'BY',
    referralUrl: 'https://www.bybit.com/register?affiliate_id=',
    benefits: ['衍生品交易量前 3', 'USDT 本位合约', '零手续费活动', '跟单交易'],
  },
  {
    name: 'Gate.io',
    logo: 'GT',
    referralUrl: 'https://www.gate.io/signup/',
    benefits: ['1500+ 币种', 'Startup IEO 平台', '量化跟单', '低门槛合约'],
  },
  {
    name: 'Bitget',
    logo: 'BG',
    referralUrl: 'https://www.bitget.com/register?from=',
    benefits: ['跟单交易第一', '合约交易大赛', '新用户福利', 'USDC 期权'],
  },
  {
    name: 'Kraken',
    logo: 'KR',
    referralUrl: 'https://www.kraken.com/sign-up',
    benefits: ['欧美合规交易所', '银行级安全', '欧元/英镑法币', '机构级 API'],
  },
]

/** Load exchange links from localStorage overrides or use fallback defaults. */
function getExchanges(): ExchangeInfo[] {
  try {
    const raw = localStorage.getItem('xt-exchange-links')
    if (raw) {
      const custom = JSON.parse(raw) as ExchangeInfo[]
      // Merge: custom links override fallback ones by name
      const merged = FALLBACK_EXCHANGES.map(fe => {
        const c = custom.find(e => e.name === fe.name)
        return c ? { ...fe, ...c } : fe
      })
      return merged
    }
  } catch { /* fall through */ }
  return FALLBACK_EXCHANGES
}

function useExchanges() {
  const [exchanges] = useState<ExchangeInfo[]>(getExchanges)
  return exchanges
}

interface Props {
  open: boolean
  onClose: () => void
}

export function ExchangeSignupModal({ open, onClose }: Props) {
  const exchanges = useExchanges()
  if (!open) return null

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={onClose} onKeyDown={(e) => { if (e.key === 'Escape') onClose() }} tabIndex={-1}>
      <div role="document" className="w-full max-w-md max-h-[80vh] overflow-y-auto rounded-2xl border border-quant-border bg-quant-card p-5 shadow-2xl" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-base font-semibold">连接交易所</h3>
          <button onClick={onClose} className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-white/5">
            <X className="h-4 w-4" />
          </button>
        </div>

        <p className="text-xs text-muted-foreground mb-4">
          选择交易所注册账号，然后在<b className="text-foreground">设置 → 交易所账户</b>中配置 API Key 即可开始交易。
        </p>

        <div className="space-y-2">
          {exchanges.map((ex) => (
            <a
              key={ex.name}
              href={ex.referralUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-start gap-3 p-3 rounded-xl border border-quant-border hover:border-quant-gold/30 hover:bg-quant-gold/[0.03] transition-all group"
            >
              <div className="w-10 h-10 rounded-xl bg-quant-bg-tertiary flex items-center justify-center text-sm font-bold shrink-0 group-hover:bg-quant-gold/10 group-hover:text-quant-gold transition-colors">
                {ex.logo}
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="text-sm font-medium">{ex.name}</span>
                  <ExternalLink className="h-3 w-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                </div>
                <div className="flex flex-wrap gap-1 mt-1">
                  {ex.benefits.map((b) => (
                    <span key={b} className="inline-flex items-center gap-0.5 text-[10px] text-muted-foreground">
                      <CheckCircle2 className="h-2.5 w-2.5 text-quant-green/70" />{b}
                    </span>
                  ))}
                </div>
              </div>
            </a>
          ))}
        </div>

        <div className="mt-4 p-3 rounded-lg bg-quant-bg-secondary text-[11px] text-muted-foreground">
          已有账号？前往 <b className="text-foreground">设置 → 交易所账户</b> 绑定 API Key
        </div>
      </div>
    </div>
  )
}

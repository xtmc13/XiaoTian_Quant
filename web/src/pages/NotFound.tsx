import { useNavigate } from 'react-router-dom'
import { Home, ArrowLeft, Compass } from 'lucide-react'

export function NotFound() {
  const navigate = useNavigate()

  return (
    <div className="h-full flex items-center justify-center bg-[#0a0a0a]">
      <div className="text-center max-w-md px-6">
        {/* 404 Illustration */}
        <div className="mb-8">
          <div className="text-[120px] font-bold leading-none text-white/5 select-none">
            404
          </div>
          <div className="mt-[-48px] text-lg font-semibold text-white">
            页面未找到
          </div>
          <p className="mt-2 text-sm text-[#8a8a8a]">
            您访问的页面不存在或已被移除。请检查 URL 是否正确。
          </p>
        </div>

        {/* Action Buttons */}
        <div className="flex flex-col sm:flex-row items-center justify-center gap-3">
          <button
            onClick={() => navigate(-1)}
            className="flex items-center gap-2 rounded-lg border border-[#1c1c1c] bg-[#111111] px-5 py-2.5 text-sm text-white transition-colors hover:bg-[#1c1c1c] hover:border-[#2a2a2a]"
          >
            <ArrowLeft className="h-4 w-4" />
            返回上页
          </button>
          <button
            onClick={() => navigate('/dashboard')}
            className="flex items-center gap-2 rounded-lg bg-white px-5 py-2.5 text-sm font-medium text-[#0a0a0a] transition-opacity hover:opacity-90"
          >
            <Home className="h-4 w-4" />
            回到首页
          </button>
          <button
            onClick={() => navigate('/indicator-community')}
            className="flex items-center gap-2 rounded-lg border border-[#1c1c1c] bg-[#111111] px-5 py-2.5 text-sm text-white transition-colors hover:bg-[#1c1c1c] hover:border-[#2a2a2a]"
          >
            <Compass className="h-4 w-4" />
            策略市场
          </button>
        </div>

        {/* Tips */}
        <div className="mt-8 pt-6 border-t border-[#1c1c1c]">
          <p className="text-[11px] text-[#666666]">
            试试这些常用页面：
          </p>
          <div className="mt-2 flex flex-wrap justify-center gap-2">
            {[
              { label: '仪表盘', path: '/dashboard' },
              { label: '交易', path: '/trading' },
              { label: '策略', path: '/strategy' },
              { label: 'AI研究', path: '/ai' },
              { label: '回测', path: '/backtest' },
              { label: '设置', path: '/settings' },
            ].map((item) => (
              <button
                key={item.path}
                onClick={() => navigate(item.path)}
                className="rounded-md bg-[#1c1c1c] px-2.5 py-1 text-[11px] text-[#8a8a8a] transition-colors hover:bg-[#262626] hover:text-white"
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

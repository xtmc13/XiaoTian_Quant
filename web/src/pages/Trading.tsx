import { useSearchParams } from 'react-router-dom'
import { SpotTrading } from './SpotTrading'
import { ContractTrading } from './ContractTrading'

export function Trading() {
  const [searchParams] = useSearchParams()
  const mode = searchParams.get('mode')

  if (mode === 'contract') {
    return <ContractTrading />
  }

  return <SpotTrading />
}

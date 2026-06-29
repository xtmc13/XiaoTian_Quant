import { useCrossArbitrage } from './useCrossArbitrage'
import { CrossArbitrageKPIs } from './CrossArbitrageKPIs'
import { CrossArbitrageControls } from './CrossArbitrageControls'
import { CrossArbitrageConfig } from './CrossArbitrageConfig'
import { CrossArbitrageOpportunities } from './CrossArbitrageOpportunities'
import { CrossArbitragePositions } from './CrossArbitragePositions'
import { CrossArbitrageHistory } from './CrossArbitrageHistory'

export function CrossArbitragePanel() {
  const {
    isRunning,
    stats,
    opportunity,
    positions,
    history,
    showHistory,
    setShowHistory,
    showConfig,
    setShowConfig,
    editConfig,
    setEditConfig,
    symbolsInput,
    setSymbolsInput,
    configuredExchanges,
    exchangesMeta,
    startMutation,
    stopMutation,
    updateConfigMut,
    registerExchangeMut,
    executeMut,
    closePositionMut,
    failPositionMut,
    handleSaveConfig,
    handleRegisterExchange,
    handleExecute,
    isPositionActive,
    handleClosePosition,
    handleFailPosition,
  } = useCrossArbitrage()

  return (
    <div className="space-y-6">
      <CrossArbitrageKPIs isRunning={isRunning} stats={stats} />
      <CrossArbitrageControls
        isRunning={isRunning}
        startMutation={startMutation}
        stopMutation={stopMutation}
        showConfig={showConfig}
        setShowConfig={setShowConfig}
        showHistory={showHistory}
        setShowHistory={setShowHistory}
      />
      {showConfig && (
        <CrossArbitrageConfig
          editConfig={editConfig}
          setEditConfig={setEditConfig}
          symbolsInput={symbolsInput}
          setSymbolsInput={setSymbolsInput}
          configuredExchanges={configuredExchanges}
          exchangesMeta={exchangesMeta}
          onSave={handleSaveConfig}
          onRegister={handleRegisterExchange}
          isSaving={updateConfigMut.isPending}
          isRegistering={registerExchangeMut.isPending}
        />
      )}
      <CrossArbitrageOpportunities
        opportunity={opportunity}
        editConfig={editConfig}
        isRunning={isRunning}
        onExecute={handleExecute}
        executeMut={executeMut}
      />
      <CrossArbitragePositions
        positions={positions}
        isPositionActive={isPositionActive}
        onClosePosition={handleClosePosition}
        onFailPosition={handleFailPosition}
        closePositionMut={closePositionMut}
        failPositionMut={failPositionMut}
      />
      {showHistory && <CrossArbitrageHistory history={history} onClose={() => setShowHistory(false)} />}
    </div>
  )
}

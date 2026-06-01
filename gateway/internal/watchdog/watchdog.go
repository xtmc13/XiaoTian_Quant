package watchdog

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ── Watchdog ──

// Component is a service monitored by the watchdog.
type Component struct {
	Name    string
	Healthy bool
	LastPing time.Time
	mu      sync.RWMutex
}

func (c *Component) Ping() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Healthy = true
	c.LastPing = time.Now()
}

func (c *Component) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Healthy
}

// Watchdog monitors component health and handles graceful shutdown.
type Watchdog struct {
	components    map[string]*Component
	maxCrashPerMin int
	shutdownCh    chan os.Signal
	restartCount   map[string]int
	lastRestart    map[string]time.Time
	OnCrash        func(name string, err error)
	OnShutdown     func()
	mu             sync.RWMutex
	stopCh         chan struct{}
	wg             sync.WaitGroup
}

func New(maxCrashPerMin int) *Watchdog {
	if maxCrashPerMin <= 0 {
		maxCrashPerMin = 3
	}
	w := &Watchdog{
		components:     make(map[string]*Component),
		maxCrashPerMin: maxCrashPerMin,
		shutdownCh:     make(chan os.Signal, 1),
		restartCount:   make(map[string]int),
		lastRestart:    make(map[string]time.Time),
		stopCh:         make(chan struct{}),
	}

	signal.Notify(w.shutdownCh, syscall.SIGINT, syscall.SIGTERM)

	go w.healthLoop()
	go w.signalHandler()

	return w
}

// Register adds a component to the watchdog.
func (w *Watchdog) Register(name string) *Component {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := &Component{Name: name, Healthy: true, LastPing: time.Now()}
	w.components[name] = c
	return c
}

// Unregister removes a component.
func (w *Watchdog) Unregister(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.components, name)
}

// RegisterRestart records a component restart and checks limits.
func (w *Watchdog) RegisterRestart(name string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	w.restartCount[name]++

	// Reset count if over 1 minute since last restart
	if last, ok := w.lastRestart[name]; ok && now.Sub(last) > time.Minute {
		w.restartCount[name] = 1
	}
	w.lastRestart[name] = now

	if w.restartCount[name] > w.maxCrashPerMin {
		log.Printf("[Watchdog] %s exceeded max restarts (%d/min), stopping recovery", name, w.maxCrashPerMin)
		return false
	}
	return true
}

// GetStatus returns the health status of all components.
func (w *Watchdog) GetStatus() map[string]any {
	w.mu.RLock()
	defer w.mu.RUnlock()
	status := make(map[string]any)
	for name, c := range w.components {
		status[name] = map[string]any{
			"healthy":     c.IsHealthy(),
			"last_ping":   c.LastPing.Format(time.RFC3339),
			"restarts":    w.restartCount[name],
		}
	}
	return status
}

// Shutdown triggers a graceful shutdown.
func (w *Watchdog) Shutdown() {
	close(w.shutdownCh)
}

// Wait blocks until shutdown is signaled.
func (w *Watchdog) Wait() {
	<-w.shutdownCh
	w.doShutdown()
}

func (w *Watchdog) healthLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.mu.RLock()
			for _, c := range w.components {
				if !c.IsHealthy() {
					log.Printf("[Watchdog] %s is unhealthy", c.Name)
					if w.OnCrash != nil {
						w.OnCrash(c.Name, fmt.Errorf("component %s unhealthy", c.Name))
					}
				}
			}
			w.mu.RUnlock()
		case <-w.stopCh:
			return
		}
	}
}

func (w *Watchdog) signalHandler() {
	sig := <-w.shutdownCh
	log.Printf("[Watchdog] Received signal: %v, initiating graceful shutdown", sig)
	w.doShutdown()
}

func (w *Watchdog) doShutdown() {
	log.Println("[Watchdog] Graceful shutdown started")
	if w.OnShutdown != nil {
		w.OnShutdown()
	}

	// Wait for components to finish
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[Watchdog] All components stopped")
	case <-time.After(30 * time.Second):
		log.Println("[Watchdog] Shutdown timeout, forcing exit")
	}
}

// Done marks a component as finished during shutdown.
func (w *Watchdog) Done() {
	w.wg.Done()
}

// Add adds to the wait group for shutdown tracking.
func (w *Watchdog) Add(delta int) {
	w.wg.Add(delta)
}

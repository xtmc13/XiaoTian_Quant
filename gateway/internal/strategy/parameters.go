package strategy

import (
	"encoding/json"
	"fmt"
	"math"
)

// ParamType defines the type of a strategy parameter.
type ParamType int

const (
	ParamInt         ParamType = iota
	ParamFloat
	ParamBool
	ParamCategorical
)

func (p ParamType) String() string {
	switch p {
	case ParamInt:
		return "INT"
	case ParamFloat:
		return "FLOAT"
	case ParamBool:
		return "BOOL"
	case ParamCategorical:
		return "CATEGORICAL"
	default:
		return "UNKNOWN"
	}
}

// Parameter defines a single hyperparameter with constraints.
type Parameter struct {
	Name        string      `json:"name"`
	Type        ParamType   `json:"type"`
	Default     any         `json:"default"`
	Min         float64     `json:"min,omitempty"`
	Max         float64     `json:"max,omitempty"`
	Step        float64     `json:"step,omitempty"`
	Options     []string    `json:"options,omitempty"` // for Categorical
	Description string      `json:"description"`
	Space       string      `json:"space"` // "buy", "sell", "roi", "stoploss", "trailing", "protection"
	Optimize    bool        `json:"optimize"` // whether to include in hyperopt
	value       any         // current value
}

// GetInt returns the parameter value as an int.
func (p *Parameter) GetInt() int {
	if p.Type != ParamInt {
		panic(fmt.Sprintf("parameter %s is not INT", p.Name))
	}
	if p.value != nil {
		return p.value.(int)
	}
	return p.Default.(int)
}

// GetFloat returns the parameter value as a float64.
func (p *Parameter) GetFloat() float64 {
	if p.Type != ParamFloat {
		panic(fmt.Sprintf("parameter %s is not FLOAT", p.Name))
	}
	if p.value != nil {
		return p.value.(float64)
	}
	return p.Default.(float64)
}

// GetBool returns the parameter value as a bool.
func (p *Parameter) GetBool() bool {
	if p.Type != ParamBool {
		panic(fmt.Sprintf("parameter %s is not BOOL", p.Name))
	}
	if p.value != nil {
		return p.value.(bool)
	}
	return p.Default.(bool)
}

// GetString returns the parameter value as a string.
func (p *Parameter) GetString() string {
	if p.Type != ParamCategorical {
		panic(fmt.Sprintf("parameter %s is not CATEGORICAL", p.Name))
	}
	if p.value != nil {
		return p.value.(string)
	}
	return p.Default.(string)
}

// SetValue sets the parameter value with validation.
func (p *Parameter) SetValue(v any) error {
	switch p.Type {
	case ParamInt:
		val, ok := toInt(v)
		if !ok {
			return fmt.Errorf("parameter %s: expected int, got %v", p.Name, v)
		}
		if p.Min != 0 || p.Max != 0 {
			if float64(val) < p.Min || float64(val) > p.Max {
				return fmt.Errorf("parameter %s: value %d out of range [%.0f, %.0f]", p.Name, val, p.Min, p.Max)
			}
		}
		p.value = val

	case ParamFloat:
		val, ok := toFloat(v)
		if !ok {
			return fmt.Errorf("parameter %s: expected float, got %v", p.Name, v)
		}
		if p.Min != 0 || p.Max != 0 {
			if val < p.Min || val > p.Max {
				return fmt.Errorf("parameter %s: value %f out of range [%f, %f]", p.Name, val, p.Min, p.Max)
			}
		}
		p.value = val

	case ParamBool:
		val, ok := v.(bool)
		if !ok {
			return fmt.Errorf("parameter %s: expected bool, got %v", p.Name, v)
		}
		p.value = val

	case ParamCategorical:
		val, ok := v.(string)
		if !ok {
			return fmt.Errorf("parameter %s: expected string, got %v", p.Name, v)
		}
		if len(p.Options) > 0 {
			found := false
			for _, opt := range p.Options {
				if opt == val {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("parameter %s: value %q not in options %v", p.Name, val, p.Options)
			}
		}
		p.value = val
	}
	return nil
}

// CurrentValue returns the current value (or default if not set).
func (p *Parameter) CurrentValue() any {
	if p.value != nil {
		return p.value
	}
	return p.Default
}

// ── Parameter constructors ─────────────────────────────────────

// IntParameter creates an integer parameter for hyperopt.
func IntParameter(name string, defaultVal int, min, max float64, space string) *Parameter {
	return &Parameter{
		Name:    name,
		Type:    ParamInt,
		Default: defaultVal,
		Min:     min,
		Max:     max,
		Step:    1,
		Space:   space,
		Optimize: true,
		value:   defaultVal,
	}
}

// FloatParameter creates a float parameter for hyperopt.
func FloatParameter(name string, defaultVal, min, max, step float64, space string) *Parameter {
	if step <= 0 {
		step = (max - min) / 10
	}
	return &Parameter{
		Name:    name,
		Type:    ParamFloat,
		Default: defaultVal,
		Min:     min,
		Max:     max,
		Step:    step,
		Space:   space,
		Optimize: true,
		value:   defaultVal,
	}
}

// BoolParameter creates a boolean parameter.
func BoolParameter(name string, defaultVal bool, space string) *Parameter {
	return &Parameter{
		Name:    name,
		Type:    ParamBool,
		Default: defaultVal,
		Space:   space,
		Optimize: true,
		value:   defaultVal,
	}
}

// CategoricalParameter creates a categorical parameter.
func CategoricalParameter(name string, defaultVal string, options []string, space string) *Parameter {
	return &Parameter{
		Name:    name,
		Type:    ParamCategorical,
		Default: defaultVal,
		Options: options,
		Space:   space,
		Optimize: true,
		value:   defaultVal,
	}
}

// ── Parameter Registry ─────────────────────────────────────────

// ParamRegistry holds all parameters for a strategy.
type ParamRegistry struct {
	params map[string]*Parameter
	order  []string // maintain registration order
}

// NewParamRegistry creates a new parameter registry.
func NewParamRegistry() *ParamRegistry {
	return &ParamRegistry{
		params: make(map[string]*Parameter),
	}
}

// Register adds a parameter to the registry.
func (r *ParamRegistry) Register(p *Parameter) {
	r.params[p.Name] = p
	r.order = append(r.order, p.Name)
}

// Get returns a parameter by name.
func (r *ParamRegistry) Get(name string) *Parameter {
	return r.params[name]
}

// Set sets a parameter value by name.
func (r *ParamRegistry) Set(name string, value any) error {
	p := r.params[name]
	if p == nil {
		return fmt.Errorf("parameter %s not found", name)
	}
	return p.SetValue(value)
}

// SetAll sets multiple parameter values from a map.
func (r *ParamRegistry) SetAll(values map[string]any) error {
	for name, val := range values {
		if err := r.Set(name, val); err != nil {
			return err
		}
	}
	return nil
}

// All returns all parameters in registration order.
func (r *ParamRegistry) All() []*Parameter {
	result := make([]*Parameter, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.params[name])
	}
	return result
}

// Optimizable returns parameters marked for hyperopt.
func (r *ParamRegistry) Optimizable() []*Parameter {
	var result []*Parameter
	for _, name := range r.order {
		p := r.params[name]
		if p.Optimize {
			result = append(result, p)
		}
	}
	return result
}

// ToHyperoptSpaces converts parameters to hyperopt search spaces.
func (r *ParamRegistry) ToHyperoptSpaces() []ParamSpaceDef {
	var spaces []ParamSpaceDef
	for _, p := range r.Optimizable() {
		if p.Min == 0 && p.Max == 0 && len(p.Options) == 0 {
			continue // no optimization range defined
		}
		spaces = append(spaces, ParamSpaceDef{
			Name:    p.Name,
			Type:    p.Type,
			Min:     p.Min,
			Max:     p.Max,
			Step:    p.Step,
			Options: p.Options,
		})
	}
	return spaces
}

// ParamSpaceDef mirrors hyperopt.ParamSpace for circular-import-free usage.
type ParamSpaceDef struct {
	Name    string
	Type    ParamType
	Min     float64
	Max     float64
	Step    float64
	Options []string
}

// Count returns the number of registered parameters.
func (r *ParamRegistry) Count() int {
	return len(r.params)
}

// ── Type conversion helpers ────────────────────────────────────

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(math.Round(val)), true
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		n, err := val.Float64()
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

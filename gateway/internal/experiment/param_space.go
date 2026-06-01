package experiment

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/xiaotian-quant/gateway/internal/indicator"
)

// ── ParamSpace ────────────────────────────────────────────────────

// ParamType defines the type of a tunable parameter.
type ParamType string

const (
	ParamInt   ParamType = "int"
	ParamFloat ParamType = "float"
	ParamBool  ParamType = "bool"
	ParamStr   ParamType = "str"
)

// ParamBound defines the search space for a single parameter.
type ParamBound struct {
	Name    string    `json:"name"`
	Type    ParamType `json:"type"`
	Min     float64   `json:"min,omitempty"`     // for int/float
	Max     float64   `json:"max,omitempty"`     // for int/float
	Step    float64   `json:"step,omitempty"`    // for int/float
	Values  []string  `json:"values,omitempty"`  // for str (enum)
	Default any       `json:"default,omitempty"`
}

// ParamSpace is a collection of parameter bounds.
type ParamSpace []ParamBound

// ToMap converts a parameter vector (flat slice) to a named map.
// The order matches the ParamSpace definition.
func (ps ParamSpace) ToMap(vector []float64) map[string]any {
	result := make(map[string]any, len(ps))
	for i, bound := range ps {
		if i >= len(vector) {
			break
		}
		v := vector[i]
		switch bound.Type {
		case ParamInt:
			result[bound.Name] = int(round(v))
		case ParamFloat:
			result[bound.Name] = roundToStep(v, bound.Step)
		case ParamBool:
			result[bound.Name] = v > 0.5
		case ParamStr:
			idx := int(math.Mod(math.Abs(v), float64(len(bound.Values))))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(bound.Values) {
				idx = len(bound.Values) - 1
			}
			result[bound.Name] = bound.Values[idx]
		}
	}
	return result
}

// FromIndicatorParseResult builds a ParamSpace from parsed indicator metadata.
func FromIndicatorParseResult(parsed indicator.ParseResult) ParamSpace {
	var space ParamSpace
	for _, p := range parsed.Params {
		bound := ParamBound{
			Name:    p.Name,
			Default: p.Default,
		}
		switch p.Type {
		case "int":
			bound.Type = ParamInt
			if p.Range != nil {
				bound.Min = p.Range.Min
				bound.Max = p.Range.Max
				bound.Step = p.Range.Step
				if bound.Step <= 0 {
					bound.Step = 1
				}
			} else {
				// Default wide range if not specified
				bound.Min = float64(p.Default.(int)) * 0.2
				if bound.Min < 1 {
					bound.Min = 1
				}
				bound.Max = float64(p.Default.(int)) * 5
				bound.Step = 1
			}
		case "float":
			bound.Type = ParamFloat
			if p.Range != nil {
				bound.Min = p.Range.Min
				bound.Max = p.Range.Max
				bound.Step = p.Range.Step
				if bound.Step <= 0 {
					bound.Step = (bound.Max - bound.Min) / 100
				}
			} else {
				bound.Min = 0.0
				bound.Max = float64(p.Default.(float64)) * 5
				if bound.Max <= 0 {
					bound.Max = 1.0
				}
				bound.Step = (bound.Max - bound.Min) / 100
			}
		case "bool":
			bound.Type = ParamBool
		case "str":
			bound.Type = ParamStr
			if len(p.Values) > 0 {
				for _, v := range p.Values {
					bound.Values = append(bound.Values, fmt.Sprint(v))
				}
			}
		}
		space = append(space, bound)
	}
	return space
}

// RandomVector generates a random parameter vector within bounds.
func (ps ParamSpace) RandomVector() []float64 {
	vec := make([]float64, len(ps))
	for i, bound := range ps {
		switch bound.Type {
		case ParamInt, ParamFloat:
			vec[i] = bound.Min + rand.Float64()*(bound.Max-bound.Min)
		case ParamBool:
			if rand.Float64() > 0.5 {
				vec[i] = 1.0
			} else {
				vec[i] = 0.0
			}
		case ParamStr:
			if len(bound.Values) > 0 {
				vec[i] = float64(rand.Intn(len(bound.Values)))
			}
		}
	}
	return vec
}

// Clip clamps a vector to valid bounds.
func (ps ParamSpace) Clip(vec []float64) []float64 {
	clipped := make([]float64, len(vec))
	copy(clipped, vec)
	for i, bound := range ps {
		if i >= len(clipped) {
			break
		}
		switch bound.Type {
		case ParamInt, ParamFloat:
			if clipped[i] < bound.Min {
				clipped[i] = bound.Min
			}
			if clipped[i] > bound.Max {
				clipped[i] = bound.Max
			}
		case ParamBool:
			if clipped[i] < 0 {
				clipped[i] = 0
			}
			if clipped[i] > 1 {
				clipped[i] = 1
			}
		case ParamStr:
			if len(bound.Values) > 0 {
				mod := float64(len(bound.Values))
				v := math.Mod(math.Abs(clipped[i]), mod)
				clipped[i] = v
			}
		}
	}
	return clipped
}

// Dimension returns the number of tunable dimensions.
func (ps ParamSpace) Dimension() int { return len(ps) }

// IsValid checks if a parameter map conforms to the space.
func (ps ParamSpace) IsValid(params map[string]any) bool {
	for _, bound := range ps {
		val, ok := params[bound.Name]
		if !ok {
			return false
		}
		switch bound.Type {
		case ParamInt:
			v, ok := toFloat64(val)
			if !ok || v < bound.Min || v > bound.Max {
				return false
			}
		case ParamFloat:
			v, ok := toFloat64(val)
			if !ok || v < bound.Min || v > bound.Max {
				return false
			}
		case ParamBool:
			_, ok := val.(bool)
			if !ok {
				return false
			}
		case ParamStr:
			s, ok := val.(string)
			if !ok {
				return false
			}
			found := false
			for _, allowed := range bound.Values {
				if allowed == s {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// ── Helpers ───────────────────────────────────────────────────────

func round(v float64) float64 {
	if v < 0 {
		return math.Ceil(v - 0.5)
	}
	return math.Floor(v + 0.5)
}

func roundToStep(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return round(v/step) * step
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	}
	return 0, false
}

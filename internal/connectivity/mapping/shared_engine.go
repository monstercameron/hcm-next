package mapping

import (
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/transformation/adapters"
)

// Normalize applies the mapping IR's documented default policy to a deep
// copy. IR.Validate historically defaulted a loop variable, leaving the
// executable rule unchanged; this boundary makes the default part of the
// value that is actually handed to the shared engine.
func Normalize(value IR) (IR, error) {
	out := value
	out.Rules = make([]Rule, len(value.Rules))
	for i, rule := range value.Rules {
		out.Rules[i] = rule
		if rule.Lookup != nil {
			out.Rules[i].Lookup = make(map[string]string, len(rule.Lookup))
			for key, mapped := range rule.Lookup {
				out.Rules[i].Lookup[key] = mapped
			}
		}
		if out.Rules[i].Null == "" {
			out.Rules[i].Null = NullError
		}
	}
	if err := out.Validate(); err != nil {
		return IR{}, err
	}
	return out, nil
}

// ExecuteShared is the mapping IR cutover. It lowers the validated mapping
// rules onto the shared transformation engine and converts its canonical text
// output back to the mapping result contract. Unsupported normalization,
// lookup, money, composition and output-shape rules are refused by the
// adapter; no second interpreter is selected as a fallback.
func ExecuteShared(value IR, input map[string]string) (Result, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return Result{}, err
	}
	for _, rule := range normalized.Rules {
		if rule.Op != OpConstant {
			if _, ok := input[rule.Source]; !ok && rule.Null == NullError {
				return Result{}, fmt.Errorf("%w: %s", ErrMissingSource, rule.Source)
			}
		}
	}
	lowered, err := adapters.LowerConnectivityRules(adapters.ConnectivityRules{
		Version: normalized.Version,
		Rules:   connectivityRules(normalized.Rules),
	})
	if err != nil {
		return Result{}, err
	}
	row := make(map[string]string, len(input))
	for _, rule := range normalized.Rules {
		if rule.Op == OpConstant {
			continue
		}
		if source, ok := input[rule.Source]; ok {
			for _, binding := range lowered.Bindings {
				if binding.Target == rule.Target {
					row[binding.SourceKey] = source
					break
				}
			}
		}
	}
	rows, err := lowered.Run([]map[string]string{row})
	if err != nil {
		return Result{}, err
	}
	texts, err := lowered.Texts(rows[0])
	if err != nil {
		return Result{}, err
	}
	fields := make([]Field, 0, len(texts))
	for _, rule := range normalized.Rules {
		text, ok := texts[rule.Target]
		if !ok {
			continue
		}
		if text == "" && rule.Null == NullError {
			return Result{}, fmt.Errorf("%w: %s: empty output", ErrTransform, rule.Target)
		}
		fields = append(fields, Field{Target: rule.Target, Value: text})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Target < fields[j].Target })
	return Result{Fields: fields, PayloadDigest: digest(normalized, fields)}, nil
}

func connectivityRules(rules []Rule) []adapters.ConnectivityRule {
	out := make([]adapters.ConnectivityRule, len(rules))
	for i, rule := range rules {
		lookup := make(map[string]string, len(rule.Lookup))
		for key, value := range rule.Lookup {
			lookup[key] = value
		}
		out[i] = adapters.ConnectivityRule{
			Source: rule.Source, Target: rule.Target, Op: adapters.ConnectivityOp(rule.Op),
			Argument: rule.Argument, Lookup: lookup, Null: adapters.ConnectivityNullPolicy(rule.Null),
		}
	}
	return out
}

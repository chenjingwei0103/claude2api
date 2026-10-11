package service

import "strings"

// ModelSpec is the Claude.ai request shape for a public model id.
type ModelSpec struct {
	Public       string
	Upstream     string
	ThinkingMode string
	Effort       string
	Known        bool
	InlineCreate bool
}

type modelFamily struct {
	id          string
	aliases     []string
	listed      []string
	useEffort   bool
	baseMode    string
	baseEffort  string
	thinkMode   string
	thinkEffort string
}

var effortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// Order matches the account model strip: base, thinking, then effort levels.
var modelFamilies = []modelFamily{
	{
		id: "claude-sonnet-5-5", aliases: []string{"claude-sonnet-5.5"},
		listed:     []string{"low", "medium", "high", "xhigh", "max"},
		useEffort:  true,
		baseMode:   "auto",
		baseEffort: "medium",
		thinkMode:  "auto", thinkEffort: "medium",
	},
	{
		id:         "claude-sonnet-5",
		listed:     []string{"low", "medium", "high", "xhigh", "max"},
		useEffort:  true,
		baseMode:   "auto",
		baseEffort: "medium",
		thinkMode:  "auto", thinkEffort: "medium",
	},
	{
		id:         "claude-sonnet-4-6",
		listed:     []string{"low", "medium", "high", "max"},
		useEffort:  true,
		baseMode:   "off",
		baseEffort: "low",
		thinkMode:  "auto", thinkEffort: "medium",
	},
	{
		id:        "claude-haiku-4-5-20251001",
		baseMode:  "off",
		thinkMode: "extended",
		useEffort: false,
	},
}

// PublicModels returns gateway model ids, including thinking and effort variants.
func PublicModels() []string {
	var out []string
	for _, family := range modelFamilies {
		out = append(out, family.id)
		out = append(out, family.id+"-thinking")
		for _, effort := range family.listed {
			out = append(out, family.id+"-thinking-"+effort)
		}
	}
	return out
}

// ResolveModel maps a public model id onto the Claude.ai completion fields.
func ResolveModel(public string) ModelSpec {
	name := strings.TrimSpace(public)
	for _, family := range modelFamilies {
		for _, alias := range family.aliases {
			name = strings.Replace(name, alias, family.id, 1)
		}
	}
	for _, family := range modelFamilies {
		if spec, ok := family.match(name); ok {
			return spec
		}
	}
	upstream := name
	mode := "off"
	if strings.HasSuffix(name, "-thinking") {
		upstream = strings.TrimSuffix(name, "-thinking")
		mode = "extended"
	}
	return ModelSpec{Public: name, Upstream: upstream, ThinkingMode: mode}
}

func (family modelFamily) match(name string) (ModelSpec, bool) {
	if name == family.id {
		return family.spec(name, family.baseMode, family.baseEffort), true
	}
	if name == family.id+"-thinking" {
		return family.spec(name, family.thinkMode, family.thinkEffort), true
	}
	prefix := family.id + "-thinking-"
	if family.useEffort && strings.HasPrefix(name, prefix) {
		effort := strings.TrimPrefix(name, prefix)
		for _, allowed := range effortLevels {
			if effort == allowed {
				return family.spec(name, family.thinkMode, effort), true
			}
		}
	}
	return ModelSpec{}, false
}

func (family modelFamily) spec(name, mode, effort string) ModelSpec {
	if !family.useEffort {
		effort = ""
	}
	return ModelSpec{
		Public:       name,
		Upstream:     family.id,
		ThinkingMode: mode,
		Effort:       effort,
		Known:        true,
		InlineCreate: true,
	}
}

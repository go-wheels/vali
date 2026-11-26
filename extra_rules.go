package vali

import (
	"fmt"
	"slices"
	"strings"
)

// The extra rule tree keeps concrete and wildcard branches separate until
// traversal reaches a matching value.
type ruleTarget struct {
	next  map[string]*ruleTarget
	chain *ruleChain
}

type ruleTargets []*ruleTarget

func (m *schema) compileExtraRules(extra map[string][]Rule) (ruleTargets, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(extra))
	for path, rules := range extra {
		if len(rules) > 0 {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)

	root := &ruleTarget{}
	for _, path := range paths {
		target, ok := m.findRoute(m.root, path, true)
		if !ok {
			return nil, fmt.Errorf("extra rules: unknown field path %q", path)
		}
		chain, err := compileRuleChain(extra[path])
		if err == nil {
			err = validateDiveType(target.typ, chain)
		}
		if err == nil {
			_, err = m.relatedRoutes(target.scope, chain)
		}
		if err != nil {
			return nil, fmt.Errorf("extra rules for %q: %w", path, err)
		}

		node := root
		for _, segment := range strings.Split(path, ".") {
			if node.next == nil {
				node.next = make(map[string]*ruleTarget)
			}
			child := node.next[segment]
			if child == nil {
				child = &ruleTarget{}
				node.next[segment] = child
			}
			node = child
		}
		node.chain = chain
	}
	return ruleTargets{root}, nil
}

func (targets ruleTargets) hasChildren() bool {
	for _, target := range targets {
		if len(target.next) > 0 {
			return true
		}
	}
	return false
}

func (targets ruleTargets) children(segment string, index bool) ruleTargets {
	var matches ruleTargets
	for _, target := range targets {
		if index {
			if wildcard := target.next["*"]; wildcard != nil {
				matches = append(matches, wildcard)
			}
		}
		if exact := target.next[segment]; exact != nil {
			matches = append(matches, exact)
		}
	}
	return matches
}

func appendRuleChain(base, extra *ruleChain) *ruleChain {
	if extra == nil {
		return base
	}
	if base == nil {
		return extra
	}
	return &ruleChain{
		checks:   slices.Concat(base.checks, extra.checks),
		bail:     base.bail || extra.bail,
		nullable: base.nullable || extra.nullable,
		numeric:  base.numeric || extra.numeric,
		elements: appendRuleChain(base.elements, extra.elements),
	}
}

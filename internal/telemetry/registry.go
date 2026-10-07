package telemetry

import (
	"errors"
	"fmt"
	"sort"
)

// Registry holds a set of named telemetry resources, keyed by label.
type Registry struct {
	resources map[string]*Resource
}

// NewRegistry builds a Registry from a list of telemetry resource configs.
// It is an error for any config to have an empty label, or for two configs
// to share a label.
func NewRegistry(confs []Config, engineVersion string) (*Registry, error) {
	resources := make(map[string]*Resource, len(confs))
	for _, conf := range confs {
		if conf.Label == "" {
			return nil, errors.New("a telemetry resource label must not be empty")
		}
		if _, exists := resources[conf.Label]; exists {
			return nil, fmt.Errorf("telemetry resource label %q is declared more than once", conf.Label)
		}

		res, err := newResource(conf, engineVersion)
		if err != nil {
			return nil, err
		}
		resources[conf.Label] = res
	}

	return &Registry{resources: resources}, nil
}

// managerKey is the type ManagerKey is an instance of, so that it cannot
// collide with keys registered by other packages via SetGeneric/GetGeneric.
type managerKey struct{}

// ManagerKey is the key a Registry is stored under via a bento manager's
// SetGeneric/GetGeneric.
var ManagerKey = managerKey{}

// Get returns the telemetry resource registered under label. A nil Registry
// is valid to call this on and behaves as if it has no resources registered.
func (r *Registry) Get(label string) (*Resource, error) {
	if r == nil {
		return nil, fmt.Errorf("telemetry resource %q was not found: no telemetry resources are registered", label)
	}

	res, exists := r.resources[label]
	if exists {
		return res, nil
	}

	known := make([]string, 0, len(r.resources))
	for l := range r.resources {
		known = append(known, l)
	}
	sort.Strings(known)
	return nil, fmt.Errorf("telemetry resource %q was not found, known labels: %v", label, known)
}

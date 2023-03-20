package ecs

import (
	"fmt"
	"reflect"
)

type resources struct {
	registry  componentRegistry[ResID]
	resources []any
}

func newResources() resources {
	return resources{
		registry:  newComponentRegistry(),
		resources: make([]any, MaskTotalBits),
	}
}

// Add adds a resource to the world.
// The resource should always be a pointer.
//
// Panics if there is already a resource of the given type.
func (r *resources) Add(res any) ResID {
	tp := reflect.TypeOf(res).Elem()
	id := r.registry.ComponentID(tp)
	if r.resources[id] != nil {
		panic(fmt.Sprintf("Resource of type %v was already added", tp))
	}
	r.resources[id] = res
	return id
}

// Get returns a pointer to the resource of the given type.
//
// Returns nil if there is no such resource.
func (r *resources) Get(id ResID) interface{} {
	return r.resources[id]
}

// Has returns whether the world has the given resource.
func (r *resources) Has(id ResID) bool {
	return r.resources[id] != nil
}
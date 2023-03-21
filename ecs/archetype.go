package ecs

import (
	"math"
	"reflect"
	"unsafe"

	"github.com/mlange-42/arche/ecs/stats"
)

// archetypeNode is a node in the archetype graph
type archetypeNode struct {
	mask      Mask
	archetype *archetype
	toAdd     []*archetypeNode
	toRemove  []*archetypeNode
}

// Creates a new archetypeNode
func newArchetypeNode(mask Mask) archetypeNode {
	return archetypeNode{
		mask:     mask,
		toAdd:    make([]*archetypeNode, MaskTotalBits),
		toRemove: make([]*archetypeNode, MaskTotalBits),
	}
}

// GetTransitionAdd returns the archetypeNode resulting from adding a component
func (a *archetypeNode) GetTransitionAdd(id ID) (*archetypeNode, bool) {
	p := a.toAdd[id]
	return p, p != nil
}

// GetTransitionRemove returns the archetypeNode resulting from removing a component
func (a *archetypeNode) GetTransitionRemove(id ID) (*archetypeNode, bool) {
	p := a.toRemove[id]
	return p, p != nil
}

// SetTransitionAdd sets the archetypeNode resulting from adding a component
func (a *archetypeNode) SetTransitionAdd(id ID, to *archetypeNode) {
	a.toAdd[id] = to
}

// SetTransitionRemove sets the archetypeNode resulting from removing a component
func (a *archetypeNode) SetTransitionRemove(id ID, to *archetypeNode) {
	a.toRemove[id] = to
}

type archetypes = pagedArr32[archetype]

// archetype represents an ECS archetype
type archetype struct {
	Mask              Mask
	Ids               []ID
	buffers           []reflect.Value
	layouts           []layout
	indices           []uint32
	entities          storage
	graphNode         *archetypeNode
	len               uint32
	cap               uint32
	capacityIncrement uint32
	access            archetypeAccess
}

type archetypeAccess struct {
	basePointer unsafe.Pointer
	layoutSize  uintptr
}

// layout specification of a component column.
type layout struct {
	pointer  unsafe.Pointer
	itemSize uintptr
}

// Get returns a pointer to the item at the given index.
func (l *layout) Get(index uintptr) unsafe.Pointer {
	if l.pointer == nil {
		return nil
	}
	return unsafe.Add(l.pointer, l.itemSize*index)
}

// Init initializes an archetype
func (a *archetype) Init(node *archetypeNode, capacityIncrement int, forStorage bool, components ...componentType) {
	var mask Mask
	if len(components) > 0 {
		a.Ids = make([]ID, len(components))
	}

	a.buffers = make([]reflect.Value, len(components))
	a.layouts = make([]layout, MaskTotalBits)
	a.indices = make([]uint32, MaskTotalBits)

	cap := 1
	if forStorage {
		cap = capacityIncrement
	}

	prev := -1
	for i, c := range components {
		if int(c.ID) <= prev {
			panic("component arguments must be sorted by ID")
		}
		prev = int(c.ID)
		mask.Set(c.ID, true)

		size, align := c.Type.Size(), uintptr(c.Type.Align())
		size = (size + (align - 1)) / align * align

		a.Ids[i] = c.ID
		a.buffers[i] = reflect.New(reflect.ArrayOf(cap, c.Type)).Elem()
		a.layouts[c.ID] = layout{
			a.buffers[i].Addr().UnsafePointer(),
			size,
		}
		a.indices[c.ID] = uint32(i)
	}
	a.entities = storage{}
	a.entities.Init(reflect.TypeOf(Entity{}), capacityIncrement, forStorage)

	a.access = archetypeAccess{
		basePointer: unsafe.Pointer(&a.layouts[0]),
		layoutSize:  unsafe.Sizeof(a.layouts[0]),
	}

	a.graphNode = node
	a.Mask = mask

	a.capacityIncrement = uint32(capacityIncrement)
	a.len = 0
	a.cap = uint32(cap)
}

// GetEntity returns the entity at the given index
func (a *archetype) GetEntity(index uintptr) Entity {
	return *(*Entity)(a.entities.Get(index))
}

// Get returns the component with the given ID at the given index
func (a *archetypeAccess) Get(index uintptr, id ID) unsafe.Pointer {
	return a.getStorage(id).Get(index)
}

func (a *archetypeAccess) getStorage(id ID) *layout {
	return (*layout)(unsafe.Add(a.basePointer, a.layoutSize*uintptr(id)))
}

// Add adds an entity with zeroed components to the archetype
func (a *archetype) Alloc(entity Entity, zero bool) uintptr {
	idx := uintptr(a.entities.Add(&entity))
	a.extend()
	if zero {
		a.ZeroAll(idx)
	}
	a.len++
	return idx
}

func (a *archetype) extend() {
	if a.cap > a.len {
		return
	}
	a.cap = a.capacityIncrement * ((a.cap + a.capacityIncrement) / a.capacityIncrement)

	for _, id := range a.Ids {
		lay := a.access.getStorage(id)
		if lay.itemSize == 0 {
			continue
		}
		index := a.indices[id]
		old := a.buffers[index]
		a.buffers[index] = reflect.New(reflect.ArrayOf(int(a.cap), old.Type().Elem())).Elem()
		lay.pointer = a.buffers[index].Addr().UnsafePointer()
		reflect.Copy(a.buffers[index], old)
	}
}

// Add adds an entity with components to the archetype
func (a *archetype) Add(entity Entity, components ...Component) uint32 {
	if len(components) != len(a.Ids) {
		panic("Invalid number of components")
	}
	idx := a.entities.Add(&entity)

	a.extend()
	a.len++
	for _, c := range components {
		lay := a.access.getStorage(c.ID)
		dst := a.access.Get(uintptr(idx), c.ID)
		if lay.itemSize == 0 {
			continue
		}
		rValue := reflect.ValueOf(c.Comp)
		src := rValue.UnsafePointer()
		a.copy(src, dst, lay.itemSize)
	}
	return idx
}

// ZeroAll resets a block of storage in all buffers.
func (a *archetype) ZeroAll(index uintptr) {
	for _, id := range a.Ids {
		a.Zero(index, id)
	}
}

// ZeroAll resets a block of storage in one buffer.
func (a *archetype) Zero(index uintptr, id ID) {
	lay := a.access.getStorage(id)
	if lay.itemSize == 0 {
		return
	}
	dst := unsafe.Add(lay.pointer, index*lay.itemSize)

	for i := uintptr(0); i < lay.itemSize; i++ {
		*(*byte)(dst) = 0
		dst = unsafe.Add(dst, 1)
	}
}

// Remove removes an entity from the archetype
func (a *archetype) Remove(index uintptr) bool {
	swapped := a.entities.Remove(index)

	oldIndex := a.len - 1
	for _, id := range a.Ids {
		lay := a.access.getStorage(id)
		o := uintptr(oldIndex)
		n := uintptr(index)

		if n == o || lay.itemSize == 0 {
			continue
		}

		src := unsafe.Add(lay.pointer, o*lay.itemSize)
		dst := unsafe.Add(lay.pointer, n*lay.itemSize)
		a.copy(src, dst, lay.itemSize)
	}

	a.len--

	return swapped
}

// Components returns the component IDs for this archetype
func (a *archetype) Components() []ID {
	return a.Ids
}

// HasComponent returns whether the archetype contains the given component ID
func (a *archetype) HasComponent(id ID) bool {
	return a.access.getStorage(id).pointer != nil
}

// Len reports the number of entities in the archetype
func (a *archetype) Len() uint32 {
	return a.len
}

// Cap reports the current capacity of the archetype
func (a *archetype) Cap() uint32 {
	return a.cap
}

// Set overwrites a component with the data behind the given pointer
func (a *archetype) Set(index uintptr, id ID, comp interface{}) unsafe.Pointer {
	lay := a.access.getStorage(id)
	dst := a.access.Get(index, id)
	if lay.itemSize == 0 {
		return dst
	}
	rValue := reflect.ValueOf(comp)

	src := rValue.UnsafePointer()
	a.copy(src, dst, lay.itemSize)
	return dst
}

// SetPointer overwrites a component with the data behind the given pointer
func (a *archetype) SetPointer(index uintptr, id ID, comp unsafe.Pointer) unsafe.Pointer {
	lay := a.access.getStorage(id)
	dst := a.access.Get(index, id)
	if lay.itemSize == 0 {
		return dst
	}

	a.copy(comp, dst, lay.itemSize)
	return dst
}

// Stats generates statistics for an archetype
func (a *archetype) Stats(reg *componentRegistry[ID]) stats.ArchetypeStats {
	ids := a.Components()
	aCompCount := len(ids)
	aTypes := make([]reflect.Type, aCompCount)
	for j, id := range ids {
		aTypes[j] = reg.ComponentType(id)
	}

	cap := int(a.Cap())
	memPerEntity := 0
	for _, id := range a.Ids {
		lay := a.access.getStorage(id)
		memPerEntity += int(lay.itemSize)
	}
	memory := cap * (int(entitySize) + memPerEntity)

	return stats.ArchetypeStats{
		Size:            int(a.Len()),
		Capacity:        cap,
		Components:      aCompCount,
		ComponentIDs:    ids,
		ComponentTypes:  aTypes,
		Memory:          memory,
		MemoryPerEntity: memPerEntity,
	}
}

// copy from one pointer to another.
func (a *archetype) copy(src, dst unsafe.Pointer, itemSize uintptr) {
	dstSlice := (*[math.MaxInt32]byte)(dst)[:itemSize:itemSize]
	srcSlice := (*[math.MaxInt32]byte)(src)[:itemSize:itemSize]
	copy(dstSlice, srcSlice)
}
